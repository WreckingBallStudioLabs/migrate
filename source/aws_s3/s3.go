package awss3

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go/aws/client"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/golang-migrate/migrate/v4/source"
)

func init() {
	source.Register("s3", &s3Driver{})
}

type s3Driver struct {
	s3client   s3iface.S3API
	bucket     string
	prefix     string
	migrations *source.Migrations
}

func initDriver(session client.ConfigProvider, path *url.URL) s3Driver {
	return s3Driver{
		bucket:     path.Host,
		prefix:     getPrefix(path.Path),
		s3client:   s3.New(session),
		migrations: source.NewMigrations(),
	}
}

// WithInstance initializes the s3 source with an an existing AWS Session.
// Important for accessing s3 buckets that are not public (most cases)
func WithInstance(sess client.ConfigProvider, folder string) (source.Driver, error) {
	u, err := url.Parse(folder)
	if err != nil {
		return nil, err
	}
	driver := initDriver(sess, u)
	err = driver.loadMigrations()
	if err != nil {
		return nil, err
	}
	return &driver, nil
}

// s3 prefix must be empty string if root of bucket
func getPrefix(path string) string {
	s := strings.Trim(path, "/")
	if len(s) > 0 {
		return s + "/"
	}
	return s
}

func (s *s3Driver) Open(folder string) (source.Driver, error) {
	u, err := url.Parse(folder)
	if err != nil {
		return nil, err
	}
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}
	driver := initDriver(sess, u)
	err = driver.loadMigrations()
	if err != nil {
		return nil, err
	}
	return &driver, nil
}

func (s *s3Driver) loadMigrations() error {
	output, err := s.s3client.ListObjects(&s3.ListObjectsInput{
		Bucket:    aws.String(s.bucket),
		Prefix:    aws.String(s.prefix),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return err
	}
	for _, object := range output.Contents {
		_, fileName := path.Split(aws.StringValue(object.Key))
		m, err := source.DefaultParse(fileName)
		if err != nil {
			continue
		}
		if !s.migrations.Append(m) {
			return fmt.Errorf("unable to parse file %v", aws.StringValue(object.Key))
		}
	}
	return nil
}

func (s *s3Driver) Close() error {
	return nil
}

func (s *s3Driver) First() (uint, error) {
	v, ok := s.migrations.First()
	if !ok {
		return 0, os.ErrNotExist
	}
	return v, nil
}

func (s *s3Driver) Prev(version uint) (uint, error) {
	v, ok := s.migrations.Prev(version)
	if !ok {
		return 0, os.ErrNotExist
	}
	return v, nil
}

func (s *s3Driver) Next(version uint) (uint, error) {
	v, ok := s.migrations.Next(version)
	if !ok {
		return 0, os.ErrNotExist
	}
	return v, nil
}

func (s *s3Driver) ReadUp(version uint) (io.ReadCloser, string, error) {
	if m, ok := s.migrations.Up(version); ok {
		return s.open(m)
	}
	return nil, "", os.ErrNotExist
}

func (s *s3Driver) ReadDown(version uint) (io.ReadCloser, string, error) {
	if m, ok := s.migrations.Down(version); ok {
		return s.open(m)
	}
	return nil, "", os.ErrNotExist
}

func (s *s3Driver) open(m *source.Migration) (io.ReadCloser, string, error) {
	key := path.Join(s.prefix, m.Raw)
	object, err := s.s3client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", err
	}
	return object.Body, m.Identifier, nil
}
