// SPDX-FileCopyrightText: 2020 Jecoz
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jecoz/diic/google"
)

func errorf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, os.Args[0]+" error: "+format+"\n", args...)
}

func exitf(format string, args ...interface{}) {
	errorf(format, args...)
	os.Exit(-1)
}

func main() {
	k := flag.String("k", "", "Google API key.")
	cx := flag.String("cx", "", "Google custom search engine ID.")
	q := flag.String("q", "", "Optional query to search for.")
	t := flag.String("t", "undefined", "Image type to search for (clipart|face|lineart|news|photo).")
	s := flag.String("s", "undefined", "Image size to search for (huge|icon|large|medium|small|xlarge|xxlarge).")
	flag.Parse()

	ctx := context.Background()
	items, err := google.NewSC(*k, *cx).SearchImages(
		ctx,
		*q,
		google.FilterImgType(*t),
		google.FilterImgSize(*s),
	)
	if err != nil {
		exitf(err.Error())
	}

	fmt.Printf("items found %d\n", len(items))
	if len(items) > 0 {
		fmt.Printf("first link found: %v\n", items[0].Link)
	}
}
