package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type stubS3 struct {
	getReceived                   *s3.GetObjectInput
	getSent                       *s3.GetObjectOutput
	putReceived                   *s3.PutObjectInput
	putSent                       *s3.PutObjectOutput
	copyReceived                  *s3.CopyObjectInput
	copySent                      *s3.CopyObjectOutput
	getError, putError, copyError error
}

func (s *stubS3) GetObject(input *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	s.getReceived = input
	return s.getSent, s.getError
}

func (s *stubS3) PutObject(input *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
	s.putReceived = input
	return s.putSent, s.putError
}

func (s *stubS3) CopyObject(input *s3.CopyObjectInput) (*s3.CopyObjectOutput, error) {
	s.copyReceived = input
	return s.copySent, s.copyError
}

type stubSQS struct {
	getReceived                         *sqs.GetQueueUrlInput
	getSent                             *sqs.GetQueueUrlOutput
	receiveReceived                     *sqs.ReceiveMessageInput
	receiveSent                         *sqs.ReceiveMessageOutput
	deleteReceived                      *sqs.DeleteMessageInput
	deleteSent                          *sqs.DeleteMessageOutput
	getError, receiveError, deleteError error
}

func (s *stubSQS) GetQueueUrl(input *sqs.GetQueueUrlInput) (*sqs.GetQueueUrlOutput, error) {
	s.getReceived = input
	return s.getSent, s.getError
}

func (s *stubSQS) ReceiveMessage(input *sqs.ReceiveMessageInput) (*sqs.ReceiveMessageOutput, error) {
	s.receiveReceived = input
	return s.receiveSent, s.receiveError
}

func (s *stubSQS) DeleteMessage(input *sqs.DeleteMessageInput) (*sqs.DeleteMessageOutput, error) {
	s.deleteReceived = input
	return s.deleteSent, s.deleteError
}

func TestHandleProcessingErr(t *testing.T) {
	expected := errors.New("TEST")
	s3Struct := stubS3{}
	s3session = &s3Struct
	handleProcessingError("FILE", &processingError{expected, 1})

	if awsErrorBucket != *s3Struct.copyReceived.Bucket {
		t.Errorf("Did not copy to correct bucket, got %v", s3Struct.copyReceived.Bucket)
	}
	if fmt.Sprintf("%v/FILE", awsPendingBucket) != *s3Struct.copyReceived.CopySource {
		t.Errorf("Did not copy from correct bucket, got %v", s3Struct.copyReceived.CopySource)
	}
	if "FILE" != *s3Struct.copyReceived.Key {
		t.Errorf("File incorrect, got %v", s3Struct.copyReceived.Key)
	}
	if "TEST" != *s3Struct.copyReceived.Metadata["Error"] {
		t.Errorf("Error incorrect, expecting TEST got %v", s3Struct.copyReceived.Metadata["Error"])
	}
	if "1" != *s3Struct.copyReceived.Metadata["Response"] {
		t.Errorf("Response code incorrect, expecting TEST, got %v", s3Struct.copyReceived.Metadata["Response"])
	}
}

func TestQueueNotFound(t *testing.T) {
	expected := errors.New("TEST")
	sqsStruct := stubSQS{
		getError: expected,
	}
	sqsSession = &sqsStruct
	filename := pollQueue()
	if filename != "" {
		t.Errorf("Did not return empty filename, got %v", filename)
	}
	if sqsStruct.getReceived != queueInput {
		t.Errorf("Did not receive correct parameters, got %v", sqsStruct.getReceived)
	}
	if sqsStruct.receiveReceived != nil {
		t.Error("Should not be calling receive yet")
	}
	if sqsStruct.deleteReceived != nil {
		t.Error("Should not be calling delete yet")
	}
}

func TestQueueReceiveError(t *testing.T) {
	expected := errors.New("TEST")
	url := "URL"
	sqsStruct := stubSQS{
		getSent:      &sqs.GetQueueUrlOutput{QueueUrl: &url},
		receiveError: expected,
	}
	sqsSession = &sqsStruct
	filename := pollQueue()
	if filename != "" {
		t.Errorf("Did not return empty filename, got %v", filename)
	}
	if sqsStruct.getReceived != queueInput {
		t.Errorf("Did not receive correct parameters, got %v", sqsStruct.getReceived)
	}
	if sqsStruct.receiveReceived == nil || *sqsStruct.receiveReceived.QueueUrl != url || *sqsStruct.receiveReceived.MaxNumberOfMessages != 1 {
		t.Errorf("Did not receive correct parameters, got %v", sqsStruct.receiveReceived)
	}
	if sqsStruct.deleteReceived != nil {
		t.Error("Should not be calling delete yet")
	}
}

func TestQueueNoMessages(t *testing.T) {
	url := "URL"
	sqsStruct := stubSQS{
		getSent:     &sqs.GetQueueUrlOutput{QueueUrl: &url},
		receiveSent: &sqs.ReceiveMessageOutput{},
	}
	sqsSession = &sqsStruct
	filename := pollQueue()
	if filename != "" {
		t.Errorf("Did not return empty filename, got %v", filename)
	}
	if sqsStruct.getReceived != queueInput {
		t.Errorf("Did not receive correct parameters, got %v", sqsStruct.getReceived)
	}
	if sqsStruct.receiveReceived == nil || *sqsStruct.receiveReceived.QueueUrl != url || *sqsStruct.receiveReceived.MaxNumberOfMessages != 1 {
		t.Errorf("Did not receive correct parameters, got %v", sqsStruct.receiveReceived)
	}
	if sqsStruct.deleteReceived != nil {
		t.Error("Should not be calling delete yet")
	}
}

func TestQueueMultipleMessages(t *testing.T) {
	url := "URL"
	sqsStruct := stubSQS{
		getSent:     &sqs.GetQueueUrlOutput{QueueUrl: &url},
		receiveSent: &sqs.ReceiveMessageOutput{Messages: []*sqs.Message{&sqs.Message{}, &sqs.Message{}}},
	}
	sqsSession = &sqsStruct
	filename := pollQueue()
	if filename != "" {
		t.Errorf("Did not return empty filename, got %v", filename)
	}
	if sqsStruct.getReceived != queueInput {
		t.Errorf("Did not receive correct parameters, got %v", sqsStruct.getReceived)
	}
	if sqsStruct.receiveReceived == nil || *sqsStruct.receiveReceived.QueueUrl != url || *sqsStruct.receiveReceived.MaxNumberOfMessages != 1 {
		t.Errorf("Did not receive correct parameters, got %v", sqsStruct.receiveReceived)
	}
	if sqsStruct.deleteReceived != nil {
		t.Error("Should not be calling delete yet")
	}
}

func TestQueueFull(t *testing.T) {
	url := "URL"
	receipt := "receipt"
	body := "body"
	sqsStruct := stubSQS{
		getSent:     &sqs.GetQueueUrlOutput{QueueUrl: &url},
		receiveSent: &sqs.ReceiveMessageOutput{Messages: []*sqs.Message{&sqs.Message{ReceiptHandle: &receipt, Body: &body}}},
	}
	sqsSession = &sqsStruct
	filename := pollQueue()
	if filename != body {
		t.Errorf("Did not return correct filename, got %v", filename)
	}
	if sqsStruct.getReceived != queueInput {
		t.Errorf("Did not receive correct parameters, got %v", sqsStruct.getReceived)
	}
	if sqsStruct.receiveReceived == nil || *sqsStruct.receiveReceived.QueueUrl != url || *sqsStruct.receiveReceived.MaxNumberOfMessages != 1 {
		t.Errorf("Did not receive correct parameters, got %v", sqsStruct.receiveReceived)
	}
	if sqsStruct.deleteReceived == nil || *sqsStruct.deleteReceived.QueueUrl != url || *sqsStruct.deleteReceived.ReceiptHandle != receipt {
		t.Errorf("Did not receive correct parameters, got %v", sqsStruct.deleteReceived)
	}
}
