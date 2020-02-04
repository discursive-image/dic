// SPDX-FileCopyrightText: 2020 Jecoz
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	"github.com/jecoz/diic/google"
	"github.com/patrickmn/go-cache"
)

func errorf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, os.Args[0]+" error: "+format+"\n", args...)
}

func exitf(format string, args ...interface{}) {
	errorf(format, args...)
	os.Exit(-1)
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

type lcache struct {
	i *cache.Cache
}

func newCache() *lcache {
	return &lcache{
		i: cache.New(time.Minute*5, time.Minute*5),
	}
}

func (c *lcache) Get(s string) (string, bool) {
	raw, ok := c.i.Get(s)
	if !ok {
		return "", ok
	}
	link, ok := raw.(string)
	return link, ok
}

func (c *lcache) Set(k, v string) {
	c.i.SetDefault(k, v)
}

const maxcc int = 10

type RecW struct {
	gsc   *google.SC
	c     int
	rec   []string
	opts  []func(url.Values)
	done  chan bool
	err   error
	cache *lcache
}

func (r *RecW) Run(ctx context.Context) {
	defer func() { r.done <- true }()
	if r.c >= len(r.rec) {
		r.err = fmt.Errorf("tried to access column %d out of %d", r.c, len(r.rec))
		return
	}

	k := r.rec[r.c]

	// Check if the cache contains the value.
	link, ok := r.cache.Get(k)
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
		return
	}
	link = items[0].Link
	r.cache.Set(k, link)
	r.rec = append(r.rec, items[0].Link)
}

func (r *RecW) Wait() {
	<-r.done
	return
}

func enqueueRecW(ctx context.Context, rx chan *RecW) {
	w := csv.NewWriter(os.Stdout)
	for {
		select {
		case <-ctx.Done():
			errorf("quitting record queue: %v", ctx.Err())
			return
		case recw := <-rx:
			recw.Wait()
			if err := recw.err; err != nil {
				errorf("unable to obtain link: %v", err)
				recw.rec = append(recw.rec, "link not available")
			}
			if err := w.Write(recw.rec); err != nil {
				errorf("unable to write record to stdout: %v", err)
			}
			w.Flush()
		}
	}
}

func handleSSearch(ctx context.Context, gsc *google.SC, in string, c int, opts ...func(url.Values)) {
	r, err := openInputFile(in)
	if err != nil {
		exitf(err.Error())
	}
	defer r.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	csvr := csv.NewReader(r)
	sem := make(chan struct{}, maxcc)
	tx := make(chan *RecW)
	cache := newCache()

	go enqueueRecW(ctx, tx)

	for {
		rec, err := csvr.Read()
		if err != nil {
			errorf("unable to read input: %v", err)
			return
		}

		rw := &RecW{
			c:     c,
			rec:   rec,
			gsc:   gsc,
			done:  make(chan bool),
			cache: cache,
		}
		ctx, cancel := context.WithTimeout(ctx, time.Second*5)
		defer cancel()
		tx <- rw // Send item though channel to preserve ordering.
		sem <- struct{}{}

		go func(rw *RecW) {
			defer func() { <-sem }()
			rw.Run(ctx) // Execute task in a different routine.
		}(rw)
	}

	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}
}

func main() {
	k := flag.String("k", "", "Google API key.")
	cx := flag.String("cx", "", "Google custom search engine ID.")
	q := flag.String("q", "", "Optional query to search for.")
	t := flag.String("t", "undefined", "Image type to search for (clipart|face|lineart|news|photo).")
	s := flag.String("s", "undefined", "Image size to search for (huge|icon|large|medium|small|xlarge|xxlarge).")
	i := flag.String("i", "-", "Input file containing the words to retrive the image of. csv encoded, use the \"c\" flag to select the proper column. If \"q\" is present, this flag is ignored. Use - for stdin.")
	c := flag.Int("c", 0, "If \"i\" is used, selects the column which will be used as word input.")
	flag.Parse()

	ctx := context.Background()
	gsc := google.NewSC(*k, *cx)
	if *q != "" {
		handleQSearch(ctx, gsc, *q, google.FilterImgType(*t), google.FilterImgSize(*s))
	} else {
		handleSSearch(ctx, gsc, *i, *c, google.FilterImgType(*t), google.FilterImgSize(*s))
	}
}
