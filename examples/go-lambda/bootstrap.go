package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-lambda-go/lambda"
)

func Handler(ctx context.Context, event interface{}) (string, error) {
	fmt.Println("Hello from Go Lambda!")
	fmt.Printf("Received event: %v\n", event)
	return "Lambda executed successfully", nil
}

func main() {
	lambda.Start(Handler)
}
