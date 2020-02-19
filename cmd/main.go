// SPDX-FileCopyrightText: 2020 Jecoz
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/jecoz/dic/google"
)

func logf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, os.Args[0]+" * "+format+"\n", args...)
}

func errorf(format string, args ...interface{}) {
	logf("error: "+format, args...)
}

func exitf(format string, args ...interface{}) {
	errorf(format, args...)
	os.Exit(1)
}

func handleQSearch(ctx context.Context, gsc *google.SC, q string, opts ...func(url.Values)) {
	items, err := gsc.SearchImages(ctx, q, opts...)
	if err != nil {
		exitf(err.Error())
	}
	switch {
	case len(items) == 0:
		fmt.Printf("no results\n")
	default:
		fmt.Println(items[0].Link)
	}
}

func openInputFile(in string) (io.ReadCloser, error) {
	if in == "-" {
		return os.Stdin, nil
	}

	file, err := os.Open(in)
	if err != nil {
		return nil, fmt.Errorf("unable to open csv input reader: %w", err)
	}
	return file, nil
}

const maxcc int = 10

type RecW struct {
	gsc   *google.SC
	c     int
	rec   []string
	opts  []func(url.Values)
	done  chan bool
	err   error
	cache *redis.Client
}

var keyPrefix = filepath.Base(os.Args[0])

func makeKey(k string) string {
	return keyPrefix + ":" + k
}

func (r *RecW) get(k string) (string, bool) {
	if r.cache == nil {
		return "", false
	}

	val, err := r.cache.Get(makeKey(k)).Result()
	if err != nil && errors.Is(err, redis.Nil) {
		// Key not set.
		return "", false
	}
	if err != nil {
		// Unexpected error.
		errorf("unable to read from cache: %v", err)
		return "", false
	}
	return val, true
}

func (r *RecW) set(k, v string) {
	if r.cache == nil {
		return
	}
	if err := r.cache.Set(makeKey(k), v, 0).Err(); err != nil {
		errorf("unable to set cache value: %v", err)
		return
	}
	return
}

func (r *RecW) Run(ctx context.Context) {
	defer func() { r.done <- true }()
	if r.c >= len(r.rec) {
		r.err = fmt.Errorf("tried to access column %d out of %d", r.c, len(r.rec))
		return
	}

	k := r.rec[r.c]

	// Check if the cache contains the value.
	link, ok := r.get(k)
	if ok {
		r.rec = append(r.rec, link)
		return
	}

	// If not, search for the image.
	items, err := r.gsc.SearchImages(ctx, k, r.opts...)
	if err != nil {
		r.err = err
		return
	}
	if len(items) == 0 {
		r.err = fmt.Errorf("no results")
		r.rec = append(r.rec, "")
		return
	}

	link = items[0].Link
	r.set(k, link)
	r.rec = append(r.rec, items[0].Link)
}

func (r *RecW) Wait() {
	<-r.done
	return
}

func enqueueRecW(rx chan *RecW, errc chan<- error) {
	w := csv.NewWriter(os.Stdout)
	for recw := range rx {
		recw.Wait()
		if err := recw.err; err != nil {
			// This is a non critical error. The log is here to
			// prevent records from being discarded silently.
			errorf("unable to obtain link: %v", err)
			continue
		}
		if err := w.Write(recw.rec); err != nil {
			errc <- fmt.Errorf("unable to write record to stdout: %w", err)
			return
		}
		w.Flush()
	}
}

func handleSSearch(ctx context.Context, gsc *google.SC, cache *redis.Client, in string, c int, opts ...func(url.Values)) {
	r, err := openInputFile(in)
	if err != nil {
		exitf(err.Error())
	}
	defer r.Close()

	csvr := csv.NewReader(r)          // the csv input reader.
	sem := make(chan struct{}, maxcc) // concurrency semaphore.
	errc := make(chan error)          // error channel, used for error reporting from writer.
	tx := make(chan *RecW)            // wrapped records transmitter.
	defer close(tx)

	go enqueueRecW(tx, errc)

	for {
		if err := func() error {
			select {
			case <-ctx.Done():
				// In case of context cancelation, close the reader first
				// and let the current searched images finish.
				return ctx.Err()
			case err := <-errc:
				// This is critical: we're no longer able to write to stdout.
				return err
			default:
				return nil
			}
		}(); err != nil {
			errorf("exiting input processing loop: %v", err)
			break
		}

		rec, err := csvr.Read()
		if err != nil && errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			errorf("unable to read input: %v", err)
			break
		}

		rw := &RecW{
			c:     c,
			rec:   rec,
			gsc:   gsc,
			done:  make(chan bool),
			cache: cache,
		}

		tx <- rw // send item though channel to preserve ordering.
		sem <- struct{}{}

		go func(rw *RecW) {
			defer func() { <-sem }()
			_ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()

			rw.Run(_ctx) // Execute task in a different routine.
		}(rw)
	}

	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}
}

const (
	envGoogleKey = "GOOGLE_SPEECH_KEY"
	envGoogleCx  = "GOOGLE_SPEECH_CX"
)

func main() {
	k := flag.String("k", os.Getenv(envGoogleKey), "Google API key.")
	cx := flag.String("cx", os.Getenv(envGoogleCx), "Google custom search engine ID.")
	q := flag.String("q", "", "Optional query to search for.")
	t := flag.String("t", "undefined", "Image type to search for (clipart|face|lineart|news|photo).")
	s := flag.String("s", "undefined", "Image size to search for (huge|icon|large|medium|small|xlarge|xxlarge).")
	i := flag.String("i", "-", "Input file containing the words to retrive the image of. csv encoded, use the \"c\" flag to select the proper column. If \"q\" is present, this flag is ignored. Use - for stdin.")
	c := flag.Int("c", 2, "If \"i\" is used, selects the column which will be used as word input.")
	raddr := flag.String("ra", "", "Redis address to connect to. If available, will be used as link cache.")
	rdb := flag.Int("rdb", 1, "Redis DB.")
	flag.Parse()

	var client *redis.Client
	if *raddr != "" {
		client = redis.NewClient(&redis.Options{
			Addr:     *raddr,
			Password: "",
			DB:       *rdb,
		})
		if _, err := client.Ping().Result(); err != nil {
			exitf("unable to connect to redis server: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt)
	go func() {
		sig := <-sigc
		logf("signal %v received, canceling", sig)
		cancel()
	}()

	gsc := google.NewSC(*k, *cx)
	if *q != "" {
		handleQSearch(ctx, gsc, *q, google.FilterImgType(*t), google.FilterImgSize(*s))
	} else {
		handleSSearch(ctx, gsc, client, *i, *c, google.FilterImgType(*t), google.FilterImgSize(*s))
	}
}
