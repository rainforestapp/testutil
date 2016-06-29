package testutil

import (
	"bytes"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/garyburd/redigo/redis"
)

func ExampleFakeRedis() {
	r := NewFakeRedis()
	defer r.Close()

	// Get connection
	conn := r.Pool.Get()

	pong, err := redis.String(conn.Do("PING"))
	if err != nil {
		// handle error
	}

	fmt.Println(pong)
	// Output:
	// PONG
}

func ExampleFakeSQS() {
	s := NewFakeSQS("fake-queue")
	defer s.Close()

	// Send message
	s.Client.SendMessage(&sqs.SendMessageInput{
		MessageBody: aws.String("Hello!"),
		QueueUrl:    &s.URL,
	})

	// Receive message
	out, err := s.Client.ReceiveMessage(&sqs.ReceiveMessageInput{
		QueueUrl:        &s.URL,
		WaitTimeSeconds: aws.Int64(3),
	})
	if err != nil {
		// handle error
	}

	fmt.Printf(*out.Messages[0].Body)
	// Output:
	// Hello!
}

func ExampleFakeS3() {
	s := NewFakeS3("mybucket")
	defer s.Close()

	bucket := "mybucket"
	key := "mykey"

	// Put object
	s.Client.PutObject(&s3.PutObjectInput{
		Bucket: &bucket,
		Key:    &key,
		Body:   bytes.NewReader([]byte("Hello!")),
	})

	// Get object
	out, err := s.Client.GetObject(&s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		// handle error
	}

	hello := make([]byte, 6)
	_, err = out.Body.Read(hello)
	if err != nil {
		// handle error
	}
	out.Body.Close()

	fmt.Println(string(hello))
	// Output:
	// Hello!
}

func ExampleShouldCrash() {
	crasher := func() {
		log.Fatalf("Crash!")
	}
	failed := false
	fail := func() {
		failed = true
	}

	// With crashing code under test
	ShouldCrash("ExampleShouldCrash", crasher, fail)

	fmt.Println(failed)
	// Output:
	// false
}

func ExampleShouldCrashFail() {
	crasher := func() {
		log.Printf("No crash!")
		os.Exit(0)
	}
	fail := func() {
		fmt.Println("failed!")
	}

	// With non-crashing code under test
	ShouldCrash("ExampleShouldCrashFail", crasher, fail)
	// Output:
	// failed!
}
