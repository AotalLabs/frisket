package main

//Version: 1.0
//Date: Feb 2017
//Author: Allan Koster-Smith

import (
	// Std library
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	_ "net/http/pprof"

	// Amazon sdk
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sqs"

	// Opentracing with Zipkin
	"github.com/opentracing/opentracing-go"
	jaeger "github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/transport/zipkin"
)

//url to your zipkin service
const zipkinURL = "http://example.com"

var fatalLog = log.New(os.Stdout, "FATAL: ", log.LstdFlags)
var infoLog = log.New(os.Stdout, "INFO: ", log.LstdFlags)
var tick = flag.Int("tick", 1, "Number of seconds to wait before suggesting to poll the queue")

var s3session s3interface
var sqsSession sqsInterface
var awsPendingBucket string
var awsDoneBucket string
var awsErrorBucket string
var queueInput *sqs.GetQueueUrlInput

type s3interface interface {
	GetObject(input *s3.GetObjectInput) (*s3.GetObjectOutput, error)
	PutObject(input *s3.PutObjectInput) (*s3.PutObjectOutput, error)
	CopyObject(input *s3.CopyObjectInput) (*s3.CopyObjectOutput, error)
}

type sqsInterface interface {
	GetQueueUrl(input *sqs.GetQueueUrlInput) (*sqs.GetQueueUrlOutput, error)
	ReceiveMessage(input *sqs.ReceiveMessageInput) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(input *sqs.DeleteMessageInput) (*sqs.DeleteMessageOutput, error)
}

type processingError struct {
	error
	code int
}

func (p *processingError) errorCode() string {
	return strconv.Itoa(p.code)
}

func main() {
	flag.Parse()

	initAWS()
	closer := initTracing()
	defer closer.Close()

	quit := make(chan struct{})
	defer close(quit)
	go initPolling(quit)

	http.HandleFunc("/health", healthCheck)
	fatalLog.Print(http.ListenAndServe(":8081", nil))
}

func healthCheck(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("Frisket", "A Go Web Server")
	rw.WriteHeader(200)
}

// Connect to the Amazon Services
func initAWS() {
	sess, err := session.NewSession(&aws.Config{Region: aws.String("ap-southeast-2")})
	if err != nil {
		fatalLog.Fatal(err.Error())
	}

	awsDoneBucket = os.Getenv("APP_SHORTCODE") + "-done"
	awsPendingBucket = os.Getenv("APP_SHORTCODE") + "-pending"
	awsErrorBucket = os.Getenv("APP_SHORTCODE") + "-error"
	queueInput = &sqs.GetQueueUrlInput{QueueName: aws.String(os.Getenv("APP_SHORTCODE"))}
	s3session = s3.New(sess)
	sqsSession = sqs.New(sess)
}

// Setup the endpoint for tracing
func initTracing() io.Closer {
	transport, err := zipkin.NewHTTPTransport(
		zipkinURL,
		zipkin.HTTPBatchSize(10),
		zipkin.HTTPLogger(jaeger.StdLogger),
	)
	if err != nil {
		fatalLog.Fatalf("Cannot initialize Zipkin HTTP transport: %v", err)
	}
	tracer, closer := jaeger.NewTracer(
		os.Getenv("APP_SHORTCODE"),
		jaeger.NewConstSampler(true),
		jaeger.NewRemoteReporter(transport, nil),
	)
	opentracing.SetGlobalTracer(tracer)
	return closer
}

// Endless loop that pulls from the queue
func initPolling(quit chan struct{}) {
	ticker := time.NewTicker(time.Duration(*tick) * time.Second)
	for {
		select {
		case <-ticker.C:
			if filename := pollQueue(); filename != "" {
				handleProcessingError(filename, processTar(filename))
			}
		case <-quit:
			ticker.Stop()
			return
		}
	}
}

// Handles any errors when interacting with SQS
func handleQueueError(err error) string {
	if err != nil {
		infoLog.Printf("Queue error %v", err.Error())
	}
	return ""
}

// Handles any errors with processing
func handleProcessingError(filename string, err *processingError) {
	if err != nil {
		infoLog.Printf("Processing error %v", err.Error())

		errorString := []byte(err.Error())
		errorString = errorString[:min(len(errorString), 2048)]

		copySource := fmt.Sprintf("%v/%v", awsPendingBucket, filename)

		params := &s3.CopyObjectInput{
			Bucket:     &awsErrorBucket,
			CopySource: &copySource,
			Key:        &filename,

			Metadata: map[string]*string{
				"Error":    aws.String(string(errorString)),
				"Response": aws.String(err.errorCode()),
			},
		}
		_, err := s3session.CopyObject(params)
		if err != nil {
			infoLog.Printf("Could not upload result, err: %v", err.Error())
		}
	}
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

// Polls the queue for messages returning the filename if successful, an empty string on no message or an error on aws error
func pollQueue() string {
	// Start trace
	pollSp := opentracing.StartSpan("Poll Queue")
	defer pollSp.Finish()

	// Get the location of the queue
	getUrlSp := opentracing.StartSpan("GetQueueUrl", opentracing.ChildOf(pollSp.Context()))
	qresp, err := sqsSession.GetQueueUrl(queueInput)
	getUrlSp.Finish()
	if err != nil {
		return handleQueueError(fmt.Errorf("Could not locate queue, err is %v", err.Error()))
	}

	// Check to see if there is a message that can be picked up
	messageParams := &sqs.ReceiveMessageInput{
		QueueUrl:            qresp.QueueUrl,
		MaxNumberOfMessages: aws.Int64(1),
	}
	receiveSp := opentracing.StartSpan("ReceiveMessage", opentracing.ChildOf(pollSp.Context()))
	messageResp, err := sqsSession.ReceiveMessage(messageParams)
	receiveSp.Finish()
	if err != nil {
		return handleQueueError(fmt.Errorf("Could not receive message, err is %v", err.Error()))
	}
	if len(messageResp.Messages) != 1 {
		return ""
	}

	// Delete the message from the queue
	deleteParams := &sqs.DeleteMessageInput{
		QueueUrl:      qresp.QueueUrl,
		ReceiptHandle: messageResp.Messages[0].ReceiptHandle,
	}
	deleteSp := opentracing.StartSpan("DeleteMessage", opentracing.ChildOf(pollSp.Context()))
	_, err = sqsSession.DeleteMessage(deleteParams)
	deleteSp.Finish()
	if err != nil {
		return handleQueueError(fmt.Errorf("Could not remove message result, err: %v", err.Error()))
	}
	return *messageResp.Messages[0].Body
}

func processTar(filename string) *processingError {
	// Start trace
	processSp := opentracing.StartSpan("Process task")
	defer processSp.Finish()

	// Make the directory for converting files
	err := os.MkdirAll("processing", os.FileMode(0755))
	if err != nil {
		return &processingError{fmt.Errorf("Could not create the processing directory, got error %v", err.Error()), 409}
	}
	defer os.RemoveAll("processing")
	// Make the directory for converted files
	err = os.MkdirAll("processed", os.FileMode(0755))
	if err != nil {
		return &processingError{fmt.Errorf("Could not create the processed directory, got error %v", err.Error()), 409}
	}
	defer os.RemoveAll("processed")

	// Stream the file from s3
	params := &s3.GetObjectInput{
		Bucket: &awsPendingBucket,
		Key:    &filename,
	}
	getObjectSp := opentracing.StartSpan("GetObject", opentracing.ChildOf(processSp.Context()))
	resp, err := s3session.GetObject(params)
	getObjectSp.Finish()
	if err != nil {
		return &processingError{fmt.Errorf("Could not find %v, err: %v", filename, err.Error()), 404}
	}
	defer resp.Body.Close()
	files, perr := decompress(resp.Body, processSp)
	if perr != nil {
		return perr
	}

	// The actual conversions
	perr = convertFiles(files, processSp)
	if perr != nil {
		return perr
	}

	// The concatenation
	processedContents, _ := ioutil.ReadDir("./processed")
	files = []string{}
	for _, f := range processedContents {
		files = append(files, "processed/"+f.Name())
	}
	stitchSp := opentracing.StartSpan("Stitching", opentracing.ChildOf(processSp.Context()))
	err = exec.Command("gs", append([]string{"-dBATCH", "-dNOPAUSE", "-dPDFFitPage", "-q", "-sOwnerPassword=reallylongandsecurepassword", "-sDEVICE=pdfwrite", "-sOutputFile=processed/" + filename + ".pdf"}, files...)...).Run()
	stitchSp.Finish()
	if err != nil {
		return &processingError{fmt.Errorf("Could not concatenate to output PDF, err: %v", err.Error()), 550}
	}

	// Upload the finished PDF to s3
	in, err := os.Open("processed/" + filename + ".pdf")
	if err != nil {
		return &processingError{fmt.Errorf("Could not find result, err: %v", err.Error()), 560}
	}
	defer in.Close()

	pdf := "application/pdf"
	putParams := &s3.PutObjectInput{
		Bucket:      &awsDoneBucket,
		Key:         aws.String(filename + ".pdf"),
		Body:        in,
		ContentType: &pdf,
	}
	putSp := opentracing.StartSpan("PutObject", opentracing.ChildOf(processSp.Context()))
	_, err = s3session.PutObject(putParams)
	putSp.Finish()
	if err != nil {
		return &processingError{fmt.Errorf("Could not upload result, err: %v", err.Error()), 560}
	}
	return nil
}

func decompress(in io.Reader, parentSp opentracing.Span) ([]string, *processingError) {
	// Decompress the file
	decompressSp := opentracing.StartSpan("Decompressing Files", opentracing.ChildOf(parentSp.Context()))
	defer decompressSp.Finish()

	gzf, err := gzip.NewReader(in)
	if err != nil {
		return nil, &processingError{fmt.Errorf("Could not decompress file, err: %v", err.Error()), 530}
	}
	tarReader := tar.NewReader(gzf)
	files := []string{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, &processingError{fmt.Errorf("Could not decompress, got error %v", err.Error()), 532}
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Left blank on purpose
		case tar.TypeReg:
			_, file := filepath.Split(header.Name)
			name := "processing/" + file
			writer, err := os.Create(name)
			if err != nil {
				return nil, &processingError{fmt.Errorf("Could not decompress file, got error %v", err.Error()), 533}
			}

			io.Copy(writer, tarReader)

			err = os.Chmod(name, os.FileMode(header.Mode))
			writer.Close()
			if err != nil {
				return nil, &processingError{fmt.Errorf("Could not change permissions got error %v", err.Error()), 534}
			}

			files = append(files, name)
		default:
			return nil, &processingError{fmt.Errorf("Unknown file type %v", header.Typeflag), 531}
		}
	}
	return files, nil
}

func convertFiles(files []string, parentSp opentracing.Span) *processingError {
	convertSp := opentracing.StartSpan("Converting Files", opentracing.ChildOf(parentSp.Context()))
	defer convertSp.Finish()
	toConvert := []string{}
	for _, file := range files {
		content, err := getFileType(file)
		if err != nil {

		}
		switch content {
		case "application/pdf":
			_, filename := filepath.Split(file)
			err = os.Link(file, "processed/"+filename)
			if err != nil {
				return &processingError{fmt.Errorf("Could not link file %v, all files: %v, err: %v", filename, files, err), 541}
			}
		case "text/html", "text/htm":
			in, err := os.Open(file)
			if err != nil {
				return &processingError{fmt.Errorf("Could not find file, err: %v", err), 540}
			}
			_, filename := filepath.Split(file)
			out, err := os.Create("processed/" + filename)
			if err != nil {
				return &processingError{fmt.Errorf("Could not create output file, err: %v", err), 540}
			}
			cmd := exec.Command("wkhtmltopdf", "--quiet", "-", "-")
			cmd.Stdin = in
			cmd.Stdout = out
			err = cmd.Run()
			in.Close()
			out.Close()
			if err != nil {
				return &processingError{fmt.Errorf("Could not write to output file, err: %v", err), 542}
			}
		default:
			toConvert = append(toConvert, file)
		}
	}

	if len(toConvert) > 0 {
		documentStripSp := opentracing.StartSpan("Dos2Unix converting", opentracing.ChildOf(convertSp.Context()))
		err := exec.Command("dos2unix", append([]string{"--quiet"}, toConvert...)...).Run()
		documentStripSp.Finish()
		if err != nil {
			return &processingError{fmt.Errorf("Could not strip files got error %v", err.Error()), 543}
		}

		documentConvertSp := opentracing.StartSpan("Libreoffice converting", opentracing.ChildOf(convertSp.Context()))
		err = exec.Command("lowriter", append([]string{"--invisible", "--convert-to", "pdf:writer_pdf_Export:UTF8", "--outdir", "processed"}, toConvert...)...).Run()
		documentConvertSp.Finish()
		if err != nil {
			return &processingError{fmt.Errorf("Could not convert files to PDF, err: %v", err), 543}
		}
	}
	return nil
}

func getFileType(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("Could not find decompressed file, got error %v", err.Error())
	}
	defer file.Close()

	// Only the first 512 bytes are used to sniff the content type.
	buffer := make([]byte, 512)
	_, err = file.Read(buffer)
	if err != nil {
		return "", err
	}

	// Reset the read pointer if necessary.
	file.Seek(0, 0)

	// Always returns a valid content-type and "application/octet-stream" if no others seemed to match.
	mediaType, _, _ := mime.ParseMediaType(http.DetectContentType(buffer))
	return mediaType, nil
}