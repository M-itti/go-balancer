package main

import (
	"fmt"
	"os"
	"time"

	"github.com/valyala/fasthttp"
)

// Custom error handling function
func handleError(ctx *fasthttp.RequestCtx, err error, statusCode int) {
	ctx.SetStatusCode(statusCode)
	fmt.Fprintf(ctx, "Error: %s\n", err.Error())
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}
	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = "app"
	}

	// Define routes
	requestHandler := func(ctx *fasthttp.RequestCtx) {
		defer func() {
			if r := recover(); r != nil {
				handleError(ctx, fmt.Errorf("internal server error"), fasthttp.StatusInternalServerError)
			}
		}()

		switch string(ctx.Path()) {
		case "/":
			handleHello(ctx, appName)
		case "/health":
			handleHealthCheck(ctx)
		case "/long_task":
			handleLongRunningTask(ctx, appName)
		default:
			ctx.Error("Unsupported path", fasthttp.StatusNotFound)
		}
	}

	fmt.Printf("Server starting on :%s\n", port)
	if err := fasthttp.ListenAndServe(":"+port, requestHandler); err != nil {
		fmt.Printf("Error in ListenAndServe: %s\n", err)
	}
}

// handleHealthCheck returns a 200 OK status for both HEAD and GET requests
func handleHealthCheck(ctx *fasthttp.RequestCtx) {
	if ctx.IsHead() || ctx.IsGet() {
		ctx.SetStatusCode(fasthttp.StatusOK)
	}
}

// handleHello returns a greeting using the APP_NAME environment variable
func handleHello(ctx *fasthttp.RequestCtx, appName string) {
	defer func() {
		if r := recover(); r != nil {
			handleError(ctx, fmt.Errorf("internal server error"), fasthttp.StatusInternalServerError)
		}
	}()

	fmt.Println("starting the work")
	// Simulate some work that could potentially fail
	if false { // Change this condition to test error handling
		panic("simulated error")
	}
	fmt.Println("finished")
	ctx.SetStatusCode(fasthttp.StatusOK)
	fmt.Fprintf(ctx, "%s! \n", appName)
}

// handleLongRunningTask simulates a long-running task by sleeping for 5 seconds
func handleLongRunningTask(ctx *fasthttp.RequestCtx, appName string) {
	defer func() {
		if r := recover(); r != nil {
			handleError(ctx, fmt.Errorf("internal server error"), fasthttp.StatusInternalServerError)
		}
	}()

	fmt.Println("starting the work")
	time.Sleep(5 * time.Second)
	fmt.Println("finished")
	ctx.SetStatusCode(fasthttp.StatusOK)
	fmt.Fprintf(ctx, "%s! \n", appName)
}
