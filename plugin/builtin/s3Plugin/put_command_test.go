package s3Plugin

import (
	"github.com/evergreen-ci/evergreen/command"
	"github.com/evergreen-ci/evergreen/model"
	"github.com/evergreen-ci/evergreen/model/artifact"
	. "github.com/smartystreets/goconvey/convey"
	"testing"
)

func TestS3PutValidateParams(t *testing.T) {

	Convey("With an s3 put command", t, func() {

		var cmd *S3PutCommand

		Convey("when validating command params", func() {

			cmd = &S3PutCommand{}

			Convey("a missing aws key should cause an error", func() {

				params := map[string]interface{}{
					"aws_secret":   "secret",
					"local_file":   "local",
					"remote_file":  "remote",
					"bucket":       "bck",
					"permissions":  "public-read",
					"content_type": "application/x-tar",
					"display_name": "test_file",
				}
				So(cmd.ParseParams(params), ShouldNotBeNil)
				So(cmd.validateParams(), ShouldNotBeNil)
			})

			Convey("a missing aws secret should cause an error", func() {

				params := map[string]interface{}{
					"aws_key":      "key",
					"local_file":   "local",
					"remote_file":  "remote",
					"bucket":       "bck",
					"permissions":  "public-read",
					"content_type": "application/x-tar",
					"display_name": "test_file",
				}
				So(cmd.ParseParams(params), ShouldNotBeNil)
				So(cmd.validateParams(), ShouldNotBeNil)

			})

			Convey("a missing local file should cause an error", func() {

				params := map[string]interface{}{
					"aws_key":      "key",
					"aws_secret":   "secret",
					"remote_file":  "remote",
					"bucket":       "bck",
					"permissions":  "public-read",
					"content_type": "application/x-tar",
					"display_name": "test_file",
				}
				So(cmd.ParseParams(params), ShouldNotBeNil)
				So(cmd.validateParams(), ShouldNotBeNil)

			})

			Convey("a missing remote file should cause an error", func() {

				params := map[string]interface{}{
					"aws_key":      "key",
					"aws_secret":   "secret",
					"local_file":   "local",
					"bucket":       "bck",
					"permissions":  "public-read",
					"content_type": "application/x-tar",
					"display_name": "test_file",
				}
				So(cmd.ParseParams(params), ShouldNotBeNil)
				So(cmd.validateParams(), ShouldNotBeNil)

			})

			Convey("a missing bucket should cause an error", func() {

				params := map[string]interface{}{
					"aws_key":      "key",
					"aws_secret":   "secret",
					"local_file":   "local",
					"remote_file":  "remote",
					"permissions":  "public-read",
					"content_type": "application/x-tar",
					"display_name": "test_file",
				}
				So(cmd.ParseParams(params), ShouldNotBeNil)
				So(cmd.validateParams(), ShouldNotBeNil)

			})

			Convey("a missing s3 permission should cause an error", func() {

				params := map[string]interface{}{
					"aws_key":      "key",
					"aws_secret":   "secret",
					"local_file":   "local",
					"remote_file":  "remote",
					"bucket":       "bck",
					"content_type": "application/x-tar",
					"display_name": "test_file",
				}
				So(cmd.ParseParams(params), ShouldNotBeNil)
				So(cmd.validateParams(), ShouldNotBeNil)

			})

			Convey("an invalid s3 permission should cause an error", func() {

				params := map[string]interface{}{
					"aws_key":      "key",
					"aws_secret":   "secret",
					"local_file":   "local",
					"remote_file":  "remote",
					"bucket":       "bck",
					"permissions":  "bleccchhhh",
					"content_type": "application/x-tar",
					"display_name": "test_file",
				}
				So(cmd.ParseParams(params), ShouldNotBeNil)
				So(cmd.validateParams(), ShouldNotBeNil)

			})

			Convey("a missing content type should cause an error", func() {

				params := map[string]interface{}{
					"aws_key":      "key",
					"aws_secret":   "secret",
					"local_file":   "local",
					"remote_file":  "remote",
					"bucket":       "bck",
					"permissions":  "private",
					"display_name": "test_file",
				}
				So(cmd.ParseParams(params), ShouldNotBeNil)
				So(cmd.validateParams(), ShouldNotBeNil)

			})

			Convey("an invalid visibility type should cause an error", func() {

				params := map[string]interface{}{
					"aws_key":      "key",
					"aws_secret":   "secret",
					"local_file":   "local",
					"remote_file":  "remote",
					"bucket":       "bck",
					"content_type": "application/x-tar",
					"permissions":  "private",
					"display_name": "test_file",
					"visibility":   "ARGHGHGHGHGH",
				}
				So(cmd.ParseParams(params), ShouldNotBeNil)
				So(cmd.validateParams(), ShouldNotBeNil)

			})

			Convey("a valid set of params should not cause an error", func() {

				params := map[string]interface{}{
					"aws_key":      "key",
					"aws_secret":   "secret",
					"local_file":   "local",
					"remote_file":  "remote",
					"bucket":       "bck",
					"permissions":  "public-read",
					"content_type": "application/x-tar",
					"display_name": "test_file",
				}
				So(cmd.ParseParams(params), ShouldBeNil)
				So(cmd.validateParams(), ShouldBeNil)
				So(cmd.AwsKey, ShouldEqual, params["aws_key"])
				So(cmd.AwsSecret, ShouldEqual, params["aws_secret"])
				So(cmd.LocalFile, ShouldEqual, params["local_file"])
				So(cmd.RemoteFile, ShouldEqual, params["remote_file"])
				So(cmd.Bucket, ShouldEqual, params["bucket"])
				So(cmd.Permissions, ShouldEqual, params["permissions"])
				So(cmd.DisplayName, ShouldEqual, params["display_name"])

			})

		})

	})
}

func TestExpandS3PutParams(t *testing.T) {

	Convey("With an s3 put command and a task config", t, func() {

		var cmd *S3PutCommand
		var conf *model.TaskConfig

		Convey("when expanding the command's params", func() {

			cmd = &S3PutCommand{}
			conf = &model.TaskConfig{
				Expansions: command.NewExpansions(map[string]string{}),
			}

			Convey("all appropriate values should be expanded, if they"+
				" contain expansions", func() {

				cmd.AwsKey = "${aws_key}"
				cmd.AwsSecret = "${aws_secret}"
				cmd.RemoteFile = "${remote_file}"
				cmd.Bucket = "${bucket}"
				cmd.ContentType = "${content_type}"
				cmd.DisplayName = "${display_name}"
				cmd.Visibility = "${visibility}"

				conf.Expansions.Update(
					map[string]string{
						"aws_key":      "key",
						"aws_secret":   "secret",
						"remote_file":  "remote",
						"bucket":       "bck",
						"content_type": "ct",
						"display_name": "file",
						"visibility":   artifact.Private,
					},
				)

				So(cmd.expandParams(conf), ShouldBeNil)
				So(cmd.AwsKey, ShouldEqual, "key")
				So(cmd.AwsSecret, ShouldEqual, "secret")
				So(cmd.RemoteFile, ShouldEqual, "remote")
				So(cmd.Bucket, ShouldEqual, "bck")
				So(cmd.ContentType, ShouldEqual, "ct")
				So(cmd.DisplayName, ShouldEqual, "file")
				So(cmd.Visibility, ShouldEqual, "private")

			})

		})

	})
}
