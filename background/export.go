package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/getsentry/sentry-go"
	"github.com/gogo/protobuf/proto"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/spf13/viper"

	"github.com/bitmark-inc/spring-app-api/protomodel"
	"github.com/bitmark-inc/spring-app-api/s3util"
	"github.com/bitmark-inc/spring-app-api/schema/facebook"
	"github.com/bitmark-inc/spring-app-api/schema/spring"
	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/bitmark-inc/spring-app-api/ziputil"
)

func writeJSON(w io.Writer, v interface{}) error {
	e := json.NewEncoder(w)
	return e.Encode(v)
}

func (b *BackgroundContext) exportStatDataFromDynamo(ctx context.Context, accountNumber string, category, period string) ([]*protomodel.Usage, error) {
	data, err := b.fbDataStore.GetFBStat(ctx, fmt.Sprintf("%s/%s-%s-stat", accountNumber, category, period), 0, time.Now().Unix(), 0)
	if err != nil {
		return nil, err
	}

	usages := make([]*protomodel.Usage, 0)
	for _, d := range data {
		var usage protomodel.Usage
		err := proto.Unmarshal(d, &usage)
		if err != nil {
			return nil, err
		}

		usages = append(usages, &usage)
	}

	return usages, nil
}

func (b *BackgroundContext) exportPostsFromDynamo(ctx context.Context, accountNumber string) ([]*protomodel.Post, error) {
	data, err := b.fbDataStore.GetFBStat(ctx, accountNumber+"/post", 0, time.Now().Unix(), 0)
	if err != nil {
		return nil, err
	}

	posts := make([]*protomodel.Post, 0)
	for _, d := range data {
		var post protomodel.Post
		err := proto.Unmarshal(d, &post)
		if err != nil {
			return nil, err
		}

		posts = append(posts, &post)
	}

	return posts, nil
}

func (b *BackgroundContext) exportReactionsFromDynamo(ctx context.Context, accountNumber string) ([]*protomodel.Reaction, error) {

	data, err := b.fbDataStore.GetFBStat(ctx, accountNumber+"/reaction", 0, time.Now().Unix(), 0)
	if err != nil {
		return nil, err
	}

	reactions := make([]*protomodel.Reaction, 0)
	for _, d := range data {
		var reaction protomodel.Reaction
		err := proto.Unmarshal(d, &reaction)
		if err != nil {
			return nil, err
		}

		reactions = append(reactions, &reaction)
	}

	return reactions, nil
}

func (b *BackgroundContext) prepareUserExportData(ctx context.Context, accountNumber, archiveID string) error {

	logEntity := log.WithField("prefix", jobPrepareDataExport)
	fs := afero.NewOsFs()

	tmpDirname, err := afero.TempDir(fs, viper.GetString("archive.workdir"), fmt.Sprintf("spring-archive-%s-", accountNumber))
	if err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
		return err
	}
	logEntity.WithField("directory", tmpDirname).Info("create temporary archive folder for spring:")
	defer fs.RemoveAll(tmpDirname)

	var fbArchives []spring.FBArchiveORM
	if err := b.ormDB.
		Where("account_number = ?", accountNumber).
		Where("processing_status = ?", store.FBArchiveStatusProcessed). // only export processed archives
		Find(&fbArchives).Error; err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
		return err
	}

	if len(fbArchives) > 0 {
		// Prepare facebook archives
		archiveFolder := path.Join(tmpDirname, "fb_archives")
		if err != fs.Mkdir(archiveFolder, os.FileMode(0755)) {
			logEntity.Error(err)
			sentry.CaptureException(err)
			return err
		}

		sess, err := session.NewSession(b.awsConf)
		if err != nil {
			logEntity.Error(err)
			sentry.CaptureException(err)
			return err
		}

		for _, a := range fbArchives {
			archivePath := path.Join(archiveFolder, fmt.Sprintf("archive-%d.zip", a.CreatedAt.Unix()))
			file, err := fs.Create(archivePath)
			if err != nil {
				log.Error(err)
				sentry.CaptureException(err)
				return err
			}

			if err := s3util.DownloadArchive(sess, viper.GetString("aws.s3.bucket"), a.FileKey, file); err != nil {
				log.Error(err)
				sentry.CaptureException(err)
				return err
			}
			file.Close()
		}

	}

	// Generate archive folder for all spring generated data
	archiveFolder := path.Join(tmpDirname, "spring_archives")
	if err != fs.Mkdir(archiveFolder, os.FileMode(0755)) {
		logEntity.Error(err)
		sentry.CaptureException(err)
		return err
	}

	// export spring generated reaction data
	{
		file, err := fs.Create(path.Join(archiveFolder, "spring_reactions.json"))
		if err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}
		reactions, err := b.exportReactionsFromDynamo(ctx, accountNumber)
		if err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}

		if err := writeJSON(file, reactions); err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}

		file.Close()
	}

	// export spring generated post data
	{
		file, err := fs.Create(path.Join(archiveFolder, "spring_posts.json"))
		if err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}
		posts, err := b.exportPostsFromDynamo(ctx, accountNumber)
		if err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}

		if err := writeJSON(file, posts); err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}

		file.Close()
	}

	// export spring generated stat data
	{
		for _, category := range []string{"post", "reaction"} {
			for _, period := range []string{"week", "year", "decade"} {
				file, err := fs.Create(path.Join(archiveFolder, fmt.Sprintf("spring_stats_%s_%s.json", category, period)))
				if err != nil {
					logEntity.Error(err)
					// sentry.CaptureException(err)
					return err
				}
				usages, err := b.exportStatDataFromDynamo(ctx, accountNumber, category, period)
				if err != nil {
					logEntity.Error(err)
					// sentry.CaptureException(err)
					return err
				}

				if err := writeJSON(file, usages); err != nil {
					logEntity.Error(err)
					// sentry.CaptureException(err)
					return err
				}

				file.Close()
			}
		}
	}

	// export all data from database
	{
		file, err := fs.Create(path.Join(archiveFolder, "spring_db_post.json"))
		if err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}

		var data []facebook.PostORM
		if err := b.ormDB.
			Where("data_owner_id = ?", accountNumber).
			Find(&data).Error; err != nil {
			logEntity.Error(err)
			return err
		}

		if err := writeJSON(file, data); err != nil {
			logEntity.Error(err)
			return err
		}

		file.Close()
	}

	// export all data from database
	{
		file, err := fs.Create(path.Join(archiveFolder, "spring_db_postmedia.json"))
		if err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}

		var data []facebook.PostMediaORM
		if err := b.ormDB.
			Where("data_owner_id = ?", accountNumber).
			Find(&data).Error; err != nil {
			logEntity.Error(err)
			return err
		}

		if err := writeJSON(file, data); err != nil {
			logEntity.Error(err)
			return err
		}

		file.Close()
	}

	// export all data from database
	{
		file, err := fs.Create(path.Join(archiveFolder, "spring_db_tag.json"))
		if err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}

		var data []facebook.TagORM
		if err := b.ormDB.
			Where("data_owner_id = ?", accountNumber).
			Find(&data).Error; err != nil {
			logEntity.Error(err)
			return err
		}

		if err := writeJSON(file, data); err != nil {
			logEntity.Error(err)
			return err
		}

		file.Close()
	}

	// export all data from database
	{
		file, err := fs.Create(path.Join(archiveFolder, "spring_db_place.json"))
		if err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}

		var data []facebook.PlaceORM
		if err := b.ormDB.
			Where("data_owner_id = ?", accountNumber).
			Find(&data).Error; err != nil {
			logEntity.Error(err)
			return err
		}

		if err := writeJSON(file, data); err != nil {
			logEntity.Error(err)
			return err
		}

		file.Close()
	}

	// export all data from database
	{
		file, err := fs.Create(path.Join(archiveFolder, "spring_db_reaction.json"))
		if err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}

		var data []facebook.ReactionORM
		if err := b.ormDB.
			Where("data_owner_id = ?", accountNumber).
			Find(&data).Error; err != nil {
			logEntity.Error(err)
			return err
		}

		if err := writeJSON(file, data); err != nil {
			logEntity.Error(err)
			return err
		}

		file.Close()
	}

	// export all data from database
	{
		file, err := fs.Create(path.Join(archiveFolder, "spring_db_comment.json"))
		if err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}

		var data []facebook.CommentORM
		if err := b.ormDB.
			Where("data_owner_id = ?", accountNumber).
			Find(&data).Error; err != nil {
			logEntity.Error(err)
			return err
		}

		if err := writeJSON(file, data); err != nil {
			logEntity.Error(err)
			return err
		}

		file.Close()
	}

	// export all data from database
	{
		file, err := fs.Create(path.Join(archiveFolder, "spring_db_friend.json"))
		if err != nil {
			logEntity.Error(err)
			// sentry.CaptureException(err)
			return err
		}

		var data []facebook.FriendORM
		if err := b.ormDB.
			Where("data_owner_id = ?", accountNumber).
			Find(&data).Error; err != nil {
			logEntity.Error(err)
			return err
		}

		if err := writeJSON(file, data); err != nil {
			logEntity.Error(err)
			return err
		}

		file.Close()
	}

	// zip the spring exporting data
	zipFile, err := afero.TempFile(fs, viper.GetString("archive.workdir"), fmt.Sprintf("spring-archive-%s-zip-", accountNumber))
	if err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
		return err
	}
	defer fs.Remove(zipFile.Name())
	defer zipFile.Close()

	if err := ziputil.Archive(tmpDirname, zipFile); err != nil {
		logEntity.Error(err)
		return err
	}

	// upload the spring exporting archive file to s3
	sess, err := session.NewSession(b.awsConf)
	if err != nil {
		logEntity.Error(err)
		return err
	}

	if _, err := zipFile.Seek(0, 0); err != nil {
		logEntity.Error(err)
		return err
	}

	archiveKey := fmt.Sprintf("%s/spring/archives/archive-%s.zip", accountNumber, archiveID)
	if err := s3util.UploadFile(sess, zipFile, archiveKey, nil); err != nil {
		logEntity.Error(err)
		return err
	}

	return b.ormDB.Model(&spring.ArchiveORM{}).Where("id = ?", archiveID).
		Update("file_key", archiveKey).Error
}
