package main

import (
	"fmt"
	"strings"

	"github.com/bitmark-inc/spring-app-api/schema/facebook"
	"github.com/bitmark-inc/spring-app-api/schema/spring"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/spf13/viper"
)

func init() {
	viper.AutomaticEnv()
	viper.SetEnvPrefix("fbm")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
}

func main() {
	db, err := gorm.Open("postgres", viper.GetString("orm.conn"))
	if err != nil {
		panic(err)
	}

	if err := db.Exec("SET search_path TO fbm").Error; err != nil {
		panic(err)
	}

	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		panic(err)
	}

	db.AutoMigrate(
		&facebook.CommentORM{},
		&facebook.FriendORM{},
		&facebook.PlaceORM{},
		&facebook.PostORM{},
		&facebook.PostMediaORM{},
		&facebook.ReactionORM{},
		&facebook.TagORM{},
		&spring.ArchiveORM{},
	)

	// The following are customized indexes for each ORM

	db.Model(facebook.PostORM{}).RemoveForeignKey("data_owner_id", "account(account_number)")
	db.Model(facebook.PostORM{}).AddForeignKey("data_owner_id", "account(account_number)", "CASCADE", "NO ACTION")

	db.Model(facebook.PostMediaORM{}).RemoveForeignKey("data_owner_id", "account(account_number)")
	db.Model(facebook.PostMediaORM{}).AddForeignKey("data_owner_id", "account(account_number)", "CASCADE", "NO ACTION")
	db.Model(facebook.PostMediaORM{}).RemoveForeignKey("post_id", "facebook_post(id)")
	db.Model(facebook.PostMediaORM{}).AddForeignKey("post_id", "facebook_post(id)", "CASCADE", "NO ACTION")

	db.Model(facebook.PlaceORM{}).RemoveForeignKey("data_owner_id", "account(account_number)")
	db.Model(facebook.PlaceORM{}).AddForeignKey("data_owner_id", "account(account_number)", "CASCADE", "NO ACTION")
	db.Model(facebook.PlaceORM{}).RemoveForeignKey("post_id", "facebook_post(id)")
	db.Model(facebook.PlaceORM{}).AddForeignKey("post_id", "facebook_post(id)", "CASCADE", "NO ACTION")

	db.Model(facebook.TagORM{}).RemoveForeignKey("data_owner_id", "account(account_number)")
	db.Model(facebook.TagORM{}).AddForeignKey("data_owner_id", "account(account_number)", "CASCADE", "NO ACTION")
	db.Model(facebook.TagORM{}).RemoveForeignKey("post_id", "facebook_post(id)")
	db.Model(facebook.TagORM{}).AddForeignKey("post_id", "facebook_post(id)", "CASCADE", "NO ACTION")
	db.Model(facebook.TagORM{}).RemoveForeignKey("friend_id", "facebook_friend(id)")
	db.Model(facebook.TagORM{}).AddForeignKey("friend_id", "facebook_friend(id)", "CASCADE", "NO ACTION")

	db.Model(facebook.FriendORM{}).RemoveForeignKey("data_owner_id", "account(account_number)")
	db.Model(facebook.FriendORM{}).AddForeignKey("data_owner_id", "account(account_number)", "CASCADE", "NO ACTION")

	db.Model(facebook.ReactionORM{}).RemoveForeignKey("data_owner_id", "account(account_number)")
	db.Model(facebook.ReactionORM{}).AddForeignKey("data_owner_id", "account(account_number)", "CASCADE", "NO ACTION")

	db.Model(facebook.CommentORM{}).RemoveForeignKey("data_owner_id", "account(account_number)")
	db.Model(facebook.CommentORM{}).AddForeignKey("data_owner_id", "account(account_number)", "CASCADE", "NO ACTION")

	db.Model(spring.ArchiveORM{}).RemoveForeignKey("account_number", "account(account_number)")
	db.Model(spring.ArchiveORM{}).AddForeignKey("account_number", "account(account_number)", "CASCADE", "NO ACTION")
	db.Model(spring.ArchiveORM{}).Where(fmt.Sprintf("status != '%s' AND status != '%s'", "FAILURE", "SUCCESS")).
		AddUniqueIndex("archive_account_unique_if_not_done", "account_number")
}
