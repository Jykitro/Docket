// The trusted supervisor uses its app binding to fetch tool replies.
// Guest WebAssembly instances receive no environment, filesystem or network access.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"docket/internal/vercelrun"
)

func main() {
	data, err := os.ReadFile("/app/eval.wasm")
	if err != nil {
		log.Fatal(err)
	}
	worker, err := vercelrun.NewWorker(context.Background(), data, os.Getenv("PLANDILLO_APP_URL"), os.Getenv("PLANDILLO_RUNNER_SECRET"))
	if err != nil {
		log.Fatal(err)
	}
	addr, err := vercelrun.ListenPort(os.Getenv("PORT"))
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal((&http.Server{Addr: addr, Handler: worker, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 40 * time.Second}).ListenAndServe())
}
