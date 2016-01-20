// testutil contains utility functions for integration-style
// tests. The S3 and SQS utilities require the fakes3 and fake_sqs
// gems, respectively, to be installed and available in the $PATH.
package testutil

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/garyburd/redigo/redis"
)

const (
	sqsEndpoint = "http://0.0.0.0:4568"
	s3Port      = "4569"
	redisPort   = "6379"
	redisTestDB = 9
)

// FakeRedis holds a redis pool for for testing. It requires a local
// redis server to be installed and running. All tests will be run on
// DB 9, which will be flushed before and after usage, so do not run
// against a server where you need values from DB 9!
type FakeRedis struct {
	Pool *redis.Pool
}

// NewFakeRedis creates sets up a redis DB for testing and returns a
// pointer to a FakeRedis object.
func NewFakeRedis() *FakeRedis {
	r := new(FakeRedis)
	r.Pool = &redis.Pool{
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", ":"+redisPort)
			if err != nil {
				log.Fatal("Error connecting to redis, is it running? Error:", err)
			}
			// Use DB 9 as a test db
			_, err = c.Do("SELECT", redisTestDB)
			if err != nil {
				return nil, err
			}

			return c, nil
		},
	}

	c := r.Pool.Get()
	c.Do("FLUSHDB")
	c.Close()

	return r
}

// Close cleans up after a redis test.
func (r *FakeRedis) Close() {
	conn := r.Pool.Get()
	conn.Do("FLUSHDB")
	conn.Close()

	r.Pool.Close()
}

// ListenRedisChan subscribes to redis channel c and signals the
// returned channel when it receives messages.
func ListenRedisChan(pool *redis.Pool, c string) chan struct{} {
	ret := make(chan struct{})
	go func() {
		psc := redis.PubSubConn{Conn: pool.Get()}
		defer psc.Close()

		psc.Subscribe(c)
		for {
			switch v := psc.Receive().(type) {
			case redis.Message:
				ret <- struct{}{}
			case redis.Subscription:
			case error:
				log.Fatal("Subscription error:", v)
			}
		}
	}()

	return ret
}

// FakeSQS holds an SQS client and queue for a fake_sqs instance. It
// requires the fake_sqs gem to be installed with the executable in
// the $PATH.
type FakeSQS struct {
	// Client is an SQS client configured to point to a fake_sqs
	// queue.
	Client *sqs.SQS

	// Session is an AWS Session that uses the fake config.
	Session *session.Session

	// URL is the URL for a fake SQS queue.
	URL string

	cmd *exec.Cmd
}

// NewFakeSQS starts a fake_sqs process and creates a queue with name
// queueName. It returns a FakeSQS object with an SQS client and a URL
// for the newly-created queue.
func NewFakeSQS(queueName string) *FakeSQS {
	s := new(FakeSQS)
	s.cmd = exec.Command("fake_sqs")
	err := s.cmd.Start()
	if err != nil {
		log.Fatal("Error starting fake_sqs, is it installed? Err:", err)
	}

	s.Session = session.New(fakeAWSConfig(sqsEndpoint))
	tryConnect := func() bool {
		s.Client = sqs.New(s.Session)
		_, err = s.Client.CreateQueue(&sqs.CreateQueueInput{
			QueueName: &queueName,
		})
		return err == nil
	}
	fail := func() {
		log.Fatal("fake_sqs failed to start in a reasonable amount of time")
	}
	WaitFor(tryConnect, fail, 10*time.Second)
	s.URL = sqsEndpoint + "/" + queueName

	return s
}

// Close cleans up after a fake_sqs process.
func (s *FakeSQS) Close() {
	if err := s.cmd.Process.Kill(); err != nil {
		fmt.Println("Error killing fake_sqs:", err)
	}
}

// FakeS3 holds a client for a fakes3 server. It requires the fakes3
// gem be installed with the executable in $PATH.
type FakeS3 struct {
	// Client is a pointer to an S3 client set up for a fakes3
	// instance.
	Client *s3.S3

	// Session is an AWS Session that uses the fake config.
	Session *session.Session

	tmpDir string
	cmd    *exec.Cmd
}

// NewFakeS3 starts a fakes3 process and creates a bucket with name
// bucketName. It returns a pointer to a FakeS3.
func NewFakeS3(bucketName string) *FakeS3 {
	s := new(FakeS3)
	s.tmpDir, _ = ioutil.TempDir("", "fakeS3")
	s.cmd = exec.Command("fakes3", "--port", s3Port, "--root", s.tmpDir)
	if err := s.cmd.Start(); err != nil {
		log.Fatal("Error starting fakes3, is it installed?", err)
	}

	tryConnect := func() bool {
		c, err := net.Dial("tcp", ":"+s3Port)
		if err == nil {
			c.Close()
			return true
		} else {
			return false
		}
	}
	fail := func() {
		log.Fatal("fakes3 failed to start in a reasonable amount of time.")
	}
	WaitFor(tryConnect, fail, 3*time.Second)

	s.Session = session.New(fakeAWSConfig("http://0.0.0.0:" + s3Port))
	s.Client = s3.New(s.Session)
	_, err := s.Client.CreateBucket(&s3.CreateBucketInput{
		Bucket: &bucketName,
	})
	if err != nil {
		log.Fatal("Error creating S3 bucket:", err)
	}

	return s
}

// Close cleans up after and kills a fakes3 instance.
func (s *FakeS3) Close() {
	if err := s.cmd.Process.Kill(); err != nil {
		fmt.Println("Error killing fake S3:", err)
	}
	if err := os.RemoveAll(s.tmpDir); err != nil {
		fmt.Println("Error cleaning up after fake S3:", err)
	}
}

// fakeAWSConfig returns a fake AWS config set up at endpoint. It is
// useful for interacting with fake SQS and S3.
func fakeAWSConfig(endpoint string) *aws.Config {
	// The client library needs access keys even though fake s3/sqs
	// don't
	os.Setenv("AWS_ACCESS_KEY", "abc123")
	os.Setenv("AWS_SECRET_KEY", "SEKRIT")
	return &aws.Config{
		Region:           aws.String("us-east-1"),
		DisableSSL:       aws.Bool(true),
		Endpoint:         &endpoint,
		S3ForcePathStyle: aws.Bool(true),
	}
}

// WaitFor runs the try function repeatedly until it returns true. If
// the try function does not return true within the timeout period,
// fail is called. WaitFor is mostly useful for checking conditions
// asynchronously in tests; it probably shouldn't be used for
// production code.
func WaitFor(try func() bool, fail func(), timeout time.Duration) {
	start := time.Now()
	for {
		if try() {
			return
		} else if time.Now().Sub(start) > timeout {
			fail()
			return
		}
	}
}
