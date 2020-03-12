package parser

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/bitmark-inc/datapod/data-parser/storage"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	gormbulk "github.com/t-tiger/gorm-bulk-insert"

	fbutil "github.com/bitmark-inc/spring-app-api/archives/facebook"
	"github.com/bitmark-inc/spring-app-api/schema/facebook"
	"github.com/bitmark-inc/spring-app-api/schema/spring"
)

var patterns = []facebook.Pattern{
	facebook.FriendsPattern,
	facebook.PostsPattern,
	facebook.ReactionsPattern,
	facebook.CommentsPattern,
	facebook.MediaPattern,
	facebook.FilesPattern,
}

// TODO: Decouple working dir, gorm, bucket name
func ParseFacebookArchive(db *gorm.DB, accountNumber, workingDir, s3Bucket, archiveID string) error {
	contextLogger := log.WithFields(log.Fields{"archive_id": archiveID})
	contextLogger.Info("start parsing archive:", archiveID)

	var archive spring.ArchiveORM
	if err := db.Model(spring.ArchiveORM{}).Where("id = ?", archiveID).First(&archive).Error; err != nil {
		sentry.CaptureException(err)
		return err
	}

	var account spring.AccountORM
	if err := db.Model(spring.AccountORM{}).Where("account_number = ?", accountNumber).First(&account).Error; err != nil {
		sentry.CaptureException(err)
		return err
	}

	var metadata struct {
		FirstActivityTimestamp int64 `json:"first_activity_timestamp"`
		LastActivityTimestamp  int64 `json:"last_activity_timestamp"`
	}

	if err := json.Unmarshal(account.Metadata, &metadata); err != nil {
		sentry.CaptureException(err)
		return err
	}

	contextLogger.
		WithField("firstActivityTimestamp", metadata.FirstActivityTimestamp).
		WithField("lastActivityTimestamp", metadata.LastActivityTimestamp).
		Info("account metadata")

	// the layout of the local dir for this task:
	// <data-owner> / facebook /
	//   archives/
	//	   <archive-file-name>.zip
	// 	   data/
	// 		 about_you/
	// 		 ads_and_businesses/
	// 		 and more...
	dataOwner := accountNumber

	localOwnerDir := filepath.Join(workingDir, dataOwner)
	localArchiveName := filepath.Base(archive.FileKey)
	localArchivePath := filepath.Join(localOwnerDir, "archive", localArchiveName)
	localUnarchivedDataDir := filepath.Join(localOwnerDir, "data")

	fs := afero.NewOsFs()
	file, err := storage.CreateFile(fs, localArchivePath)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	defer file.Close()
	defer fs.RemoveAll(localOwnerDir)

	if err := storage.DownloadArchiveFromS3(s3Bucket, archive.FileKey, file); err != nil {
		sentry.CaptureException(err)
		return err
	}
	contextLogger.Info("archive downloaded")

	if !fbutil.IsValidArchiveFile(localArchivePath) {
		return fmt.Errorf("invalid archive file")
	}

	for _, pattern := range patterns {
		contextLogger.WithField("type", pattern.Name).Info("parsing and inserting records into db")

		if err := storage.ExtractArchive(localArchivePath, pattern.Location, localUnarchivedDataDir); err != nil {
			sentry.CaptureException(err)
			return err
		}

		subDir := filepath.Join(localUnarchivedDataDir, pattern.Location)
		if pattern.Name == "media" || pattern.Name == "files" {
			contextLogger.Info("uploading ", pattern.Name, " files to ", fmt.Sprintf("%s/facebook/archives/%s/data", dataOwner, fmt.Sprint(archive.ID)))

			if err := storage.UploadDirToS3(s3Bucket, fmt.Sprintf("%s/facebook/archives/%s/data", dataOwner, fmt.Sprint(archive.ID)), subDir); err != nil {
				sentry.CaptureException(err)
				continue
			}
		} else {
			files, err := pattern.SelectFiles(fs, subDir)
			if err != nil {
				sentry.CaptureException(err)
				return err
			}
			for _, file := range files {
				data, err := afero.ReadFile(fs, file)
				if err != nil {
					sentry.CaptureException(err)
					return err
				}

				if err := pattern.Validate(data); err != nil {
					sentry.CaptureException(err)
					return err
				}

				switch pattern.Name {
				case "friends":
					rawFriends := &facebook.RawFriends{}
					json.Unmarshal(data, &rawFriends)
					if err := gormbulk.BulkInsert(db, rawFriends.ORM(dataOwner), 500); err != nil {
						// friends must exist for inserting tags
						// stop processing if it fails to insert friends
						sentry.CaptureException(err)
						return err
					}
				case "posts":
					rawPosts := facebook.RawPosts{Items: make([]*facebook.RawPost, 0)}
					json.Unmarshal(data, &rawPosts.Items)
					posts, complexPosts := rawPosts.ORM(dataOwner, fmt.Sprint(archive.ID),
						metadata.FirstActivityTimestamp, metadata.LastActivityTimestamp)
					if err := gormbulk.BulkInsert(db, posts, 500); err != nil {
						sentry.CaptureException(err)
						continue
					}
					for _, p := range complexPosts {
						if len(p.Tags) > 0 {
							friends := make([]facebook.FriendORM, 0)
							if err := db.Where("data_owner_id = ?", dataOwner).Find(&friends).Error; err != nil {
								// friends must exist for inserting tags
								// deal with the next post if it fails to find friends of this data owner
								sentry.CaptureException(err)
								continue
							}

							friendIDs := make(map[string]uuid.UUID)
							for _, f := range friends {
								friendIDs[f.FriendName] = f.ID
							}

							// FIXME: non-friends couldn't be tagged
							c := 0 // valid tag count
							for i := range p.Tags {
								friendID, ok := friendIDs[p.Tags[i].FriendName]
								if ok {
									p.Tags[i].FriendID = friendID
									c++
								}
							}
							p.Tags = p.Tags[:c]
						}

						if err := db.Create(&p).Error; err != nil {
							sentry.CaptureException(err)
							continue
						}
					}
				case "comments":
					rawComments := &facebook.RawComments{}
					json.Unmarshal(data, &rawComments)
					if err := gormbulk.BulkInsert(db, rawComments.ORM(dataOwner), 500); err != nil {
						sentry.CaptureException(err)
						continue
					}
				case "reactions":
					rawReactions := &facebook.RawReactions{}
					json.Unmarshal(data, &rawReactions)
					if err := gormbulk.BulkInsert(db, rawReactions.ORM(dataOwner), 500); err != nil {
						sentry.CaptureException(err)
						continue
					}
				}
			}
		}

		fs.RemoveAll(subDir)
	}

	contextLogger.Info("task finished")
	return nil
}
