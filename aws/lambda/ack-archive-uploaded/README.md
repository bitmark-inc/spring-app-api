# Lambda function for ack archive uploaded

This is a lambda function that receive S3 file created events and submit
a secret api request to notify our spring api server that an given file
has uploaded to S3.

In order to use this function, you need to setup a lambda function in AWS and
set its trigger with the S3 created event.

## Environment Variables

- SERVER_URL `# https://fbm.test.bitmark.com`
- SERVER_API_TOKEN `# API Token for admin`

## Build

```
# go build
```

## Update lambda function

Suppose you have create the lambda function. Make sure the hanlder is set to the binary name, for example `ack-archive-uploaded`.

Archive the binary by zip and use awscli to upload it to AWS Lambda.

```
# zip lambda.zip ack-archive-uploaded
# aws lambda update-function-code \
    --function-name spring-ack-archive-uploaded-testnet \
    --zip-file fileb://lambda.zip
```
