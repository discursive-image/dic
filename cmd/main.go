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
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/discursive-image/dic/google"
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

type touchedImage struct {
	image   *google.ISR
	checked bool
	valid   bool
}

type imageRing struct {
	all   []*touchedImage
	index int
}

var fastClient = &http.Client{
	Timeout: 2 * time.Second,
}

func discard(link string) bool {
	resp, err := fastClient.Head(link)
	if err != nil {
		return true
	}
	resp.Body.Close()

	// Discard we do not get a positive HTTP response.
	if resp.StatusCode >= 400 {
		return true
	}
	// If content type is not image, discard.
	t := resp.Header.Get("content-type")
	if !strings.Contains(t, "image") {
		return true
	}
	// If content-length is not greater than 0, discard.
	l, err := strconv.Atoi(resp.Header.Get("content-length"))
	if err != nil || l <= 0 {
		return true
	}
	return false
}

func (ir *imageRing) next() *google.ISR {
	if len(ir.all) == 0 {
		return nil
	}

	// Lazily check images before returning them.

	var ti *touchedImage
	var found bool
	var index int
	for i := ir.index; i < len(ir.all); i = (i + 1) % (len(ir.all) - 1) {
		ti = ir.all[i]
		if !ti.checked {
			ti.valid = !discard(ti.image.Link)
		}
		if ti.valid {
			found = true
			index = i
			break
		}
	}
	if !found {
		return nil
	}
	ir.index = (index + 1) % (len(ir.all) - 1)
	return ti.image
}

type ringCache struct {
	sync.Mutex
	m map[string]*imageRing
}

func newRingCache() *ringCache {
	return &ringCache{
		m: make(map[string]*imageRing),
	}
}

func (c *ringCache) next(k string) (*google.ISR, bool) {
	c.Lock()
	defer c.Unlock()

	ring, ok := c.m[k]
	if !ok {
		return nil, false
	}
	image := ring.next()
	if image == nil {
		// something is broken with this ring, delete it.
		delete(c.m, k)
		return nil, false
	}
	return image, true
}

func (c *ringCache) set(k string, results []*google.ISR) {
	c.Lock()
	defer c.Unlock()

	all := make([]*touchedImage, len(results))
	for i, v := range results {
		all[i] = &touchedImage{
			image: v,
		}
	}
	c.m[k] = &imageRing{
		all:   all,
		index: 0,
	}
}

type ImageRequest struct {
	gsc   *google.SC
	c     int
	rec   []string
	opts  []func(url.Values)
	done  chan bool
	err   error
	cache *ringCache
}

func (r *ImageRequest) Run(ctx context.Context) {
	defer func() { r.done <- true }()
	if r.c >= len(r.rec) {
		r.err = fmt.Errorf("tried to access column %d out of %d", r.c, len(r.rec))
		return
	}

	k := r.rec[r.c]

	// Check if the cache contains the value.
	image, ok := r.cache.next(k)
	if ok {
		r.rec = append(r.rec, image.Link)
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
	r.cache.set(k, items)

	image, ok = r.cache.next(k)
	if !ok {
		r.err = fmt.Errorf("cache inconsistency")
		r.rec = append(r.rec, "")
		return
	}
	r.rec = append(r.rec, image.Link)
}

func (r *ImageRequest) Wait() {
	<-r.done
	return
}

func enqueueImageRequest(rx chan *ImageRequest, errc chan<- error) {
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

func handleSSearch(ctx context.Context, gsc *google.SC, in string, c int, opts ...func(url.Values)) {
	r, err := openInputFile(in)
	if err != nil {
		exitf(err.Error())
	}
	defer r.Close()

	csvr := csv.NewReader(r)          // the csv input reader.
	sem := make(chan struct{}, maxcc) // concurrency semaphore.
	errc := make(chan error)          // error channel, used for error reporting from writer.
	tx := make(chan *ImageRequest)    // wrapped records transmitter.
	cache := newRingCache()
	defer close(tx)

	go enqueueImageRequest(tx, errc)

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

		rw := &ImageRequest{
			c:     c,
			rec:   rec,
			gsc:   gsc,
			done:  make(chan bool),
			cache: cache,
		}

		tx <- rw // send item though channel to preserve ordering.
		sem <- struct{}{}

		go func(rw *ImageRequest) {
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
	envGoogleKey = "GOOGLE_SEARCH_KEY"
	envGoogleCx  = "GOOGLE_SEARCH_CX"
)

func main() {
	k := flag.String("k", os.Getenv(envGoogleKey), "Google API key.")
	cx := flag.String("cx", os.Getenv(envGoogleCx), "Google custom search engine ID.")
	q := flag.String("q", "", "Optional query to search for.")
	t := flag.String("t", "undefined", "Image type to search for (clipart|face|lineart|news|photo).")
	s := flag.String("s", "undefined", "Image size to search for (huge|icon|large|medium|small|xlarge|xxlarge).")
	i := flag.String("i", "-", "Input file containing the words to retrive the image of. csv encoded, use the \"c\" flag to select the proper column. If \"q\" is present, this flag is ignored. Use - for stdin.")
	c := flag.Int("c", 3, "If \"i\" is used, selects the column which will be used as word input.")
	flag.Parse()

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
		handleSSearch(ctx, gsc, *i, *c, google.FilterImgType(*t), google.FilterImgSize(*s))
	}
}
