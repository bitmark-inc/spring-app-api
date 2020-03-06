package main

import (
	"context"
	"strconv"

	"github.com/RichardKnop/machinery/v1/tasks"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/bitmark-inc/spring-app-api/background/parser"
	"github.com/bitmark-inc/spring-app-api/store"
)

// parseArchive parse archive data based on its type
func (b *BackgroundContext) parseArchive(ctx context.Context, archiveType, accountNumber string, archiveID int64) error {
	if _, err := b.store.UpdateFBArchiveStatus(ctx, &store.FBArchiveQueryParam{
		ID: &archiveID,
	}, &store.FBArchiveQueryParam{
		Status: &store.FBArchiveStatusProcessing,
	}); err != nil {
		log.Error(err)
		return err
	}

	switch archiveType {
	case "facebook":
		if err := parser.ParseFacebookArchive(b.ormDB,
			accountNumber, viper.GetString("archive.workdir"),
			viper.GetString("aws.s3.bucket"),
			strconv.FormatInt(archiveID, 10)); err != nil {
			return err
		}
	}

	_, err := server.SendTask(&tasks.Signature{
		Name: "analyze_posts",
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: accountNumber,
			},
			{
				Type:  "int64",
				Value: archiveID,
			},
		},
	})

	if err != nil {
		log.Debug(err)
		return err
	}

	return nil
}
