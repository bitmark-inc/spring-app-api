package parser

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	gormbulk "github.com/t-tiger/gorm-bulk-insert"

	fbutil "github.com/bitmark-inc/spring-app-api/archives/facebook"
	"github.com/bitmark-inc/spring-app-api/s3util"
	"github.com/bitmark-inc/spring-app-api/schema/facebook"
	"github.com/bitmark-inc/spring-app-api/schema/spring"
	"github.com/bitmark-inc/spring-app-api/ziputil"
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
func ParseFacebookArchive(sess *session.Session, db *gorm.DB, accountNumber, workingDir, s3Bucket, archiveID string) error {
	contextLogger := log.WithFields(log.Fields{"archive_id": archiveID})
	contextLogger.Info("start parsing archive:", archiveID)

	var archive spring.FBArchiveORM
	if err := db.Model(spring.FBArchiveORM{}).Where("id = ?", archiveID).First(&archive).Error; err != nil {
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

	localOwnerDir := filepath.Join(workingDir, dataOwner, archiveID)
	localArchiveName := filepath.Base(archive.FileKey)
	localArchivePath := filepath.Join(localOwnerDir, "archive", localArchiveName)
	localUnarchivedDataDir := filepath.Join(localOwnerDir, "data")

	fs := afero.NewOsFs()
	if err := fs.MkdirAll(filepath.Dir(localArchivePath), os.FileMode(0777)); err != nil {
		sentry.CaptureException(err)
		return err
	}
	file, err := fs.Create(localArchivePath)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}

	defer file.Close()
	defer fs.RemoveAll(localOwnerDir)

	if err := s3util.DownloadArchive(sess, s3Bucket, archive.FileKey, file); err != nil {
		sentry.CaptureException(err)
		return err
	}
	contextLogger.Info("archive downloaded")

	if !fbutil.IsValidArchiveFile(localArchivePath) {
		return fmt.Errorf("invalid archive file")
	}

	for _, pattern := range patterns {
		contextLogger.WithField("type", pattern.Name).Info("parsing and inserting records into db")

		if err := ziputil.Extract(localArchivePath, localUnarchivedDataDir, pattern.Location); err != nil {
			sentry.CaptureException(err)
			return err
		}

		subDir := filepath.Join(localUnarchivedDataDir, pattern.Location)
		if pattern.Name == "media" || pattern.Name == "files" {
			contextLogger.Info("uploading ", pattern.Name, " files to ", fmt.Sprintf("%s/facebook/archives/%s/data", dataOwner, fmt.Sprint(archive.ID)))

			if err := s3util.UploadDir(sess, s3Bucket, fmt.Sprintf("%s/facebook/archives/%s/data", dataOwner, fmt.Sprint(archive.ID)), subDir); err != nil {
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
					if err := json.Unmarshal(data, &rawPosts.Items); err != nil {
						sentry.CaptureException(err)
						return err
					}
					posts, complexPosts := rawPosts.ORM(dataOwner, fmt.Sprint(archive.ID),
						metadata.FirstActivityTimestamp, metadata.LastActivityTimestamp)
					if err := gormbulk.BulkInsert(db, posts, 500); err != nil {
						sentry.CaptureException(err)
					}

					for _, p := range complexPosts {
						postTags := p.Tags
						postMedia := p.MediaItems
						postPlaces := p.Places

						p.Tags = nil
						p.MediaItems = nil
						p.Places = nil
						if err := db.Set("gorm:insert_option", "ON CONFLICT (timestamp, data_owner_id) DO UPDATE set conflict_flag = true").
							Create(&p).Error; err != nil {
							contextLogger.Debug(err)
							sentry.CaptureException(err)
						}

						if len(postTags) > 0 {
							friends := make([]facebook.FriendORM, 0)
							if err := db.Where("data_owner_id = ?", dataOwner).Find(&friends).Error; err != nil {
								// friends must exist for inserting tags
								// deal with the next post if it fails to find friends of this data owner
								contextLogger.Error(err)
								sentry.CaptureException(err)
							}

							friendIDs := make(map[string]uuid.UUID)
							for _, f := range friends {
								friendIDs[f.FriendName] = f.ID
							}

							// FIXME: non-friends couldn't be tagged
							for _, tag := range postTags {
								friendID, ok := friendIDs[tag.FriendName]
								if ok {
									tag.FriendID = friendID
									tag.PostID = p.ID
									if err := db.Create(&tag).Error; err != nil {
										if err != sql.ErrNoRows {
											contextLogger.Error(err)
											sentry.CaptureException(err)
										}
									}
								}
							}
						}

						if len(postMedia) > 0 {
							for _, m := range postMedia {
								var currentMedia facebook.PostMediaORM
								if err := db.Where("timestamp = ? AND media_index = ? AND data_owner_id = ? AND post_id = ?",
									m.Timestamp, m.MediaIndex, m.DataOwnerID, p.ID).
									First(&currentMedia).Error; err != nil {
									if gorm.IsRecordNotFoundError(err) {
										m.PostID = p.ID
									} else {
										contextLogger.Error(err)
										sentry.CaptureException(err)
									}
								} else {
									m.ID = currentMedia.ID
									m.PostID = currentMedia.PostID
								}
								if err := db.Save(&m).Error; err != nil {
									contextLogger.Error(err)
									sentry.CaptureException(err)
								}
							}
						}

						if len(postPlaces) > 0 {
							for _, place := range postPlaces {
								place.PostID = p.ID
								if err := db.Create(&place).Error; err != nil {
									if err != sql.ErrNoRows {
										contextLogger.Error(err)
										sentry.CaptureException(err)
									}
								}
							}
						}
					}
				case "comments":
					rawComments := &facebook.RawComments{}
					if err := json.Unmarshal(data, &rawComments); err != nil {
						sentry.CaptureException(err)
						return err
					}
					comments, complexComments := rawComments.ORM(dataOwner, archiveID)
					if err := gormbulk.BulkInsert(db, comments, 500); err != nil {
						sentry.CaptureException(err)
						continue
					}
					for _, comment := range complexComments {
						commentMedia := comment.MediaItems
						comment.MediaItems = nil
						if err := db.Set("gorm:insert_option", "ON CONFLICT (timestamp, data_owner_id) DO UPDATE set conflict_flag = true").
							Create(&comment).Error; err != nil {
							contextLogger.Debug(err)
							sentry.CaptureException(err)
						}

						for _, m := range commentMedia {
							var currentMedia facebook.CommentMediaORM
							if err := db.Where("timestamp = ? AND media_index = ? AND data_owner_id = ? AND comment_id = ?",
								m.Timestamp, m.MediaIndex, m.DataOwnerID, comment.ID).
								First(&currentMedia).Error; err != nil {
								if gorm.IsRecordNotFoundError(err) {
									m.CommentID = comment.ID
								} else {
									contextLogger.Debug(err)
									sentry.CaptureException(err)
								}
							} else {
								m.ID = currentMedia.ID
								m.CommentID = comment.ID
							}
							if err := db.Save(&m).Error; err != nil {
								contextLogger.Debug(err)
								sentry.CaptureException(err)
							}
						}
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
