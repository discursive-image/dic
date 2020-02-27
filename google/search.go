// SPDX-FileCopyrightText: 2020 Jecoz
//
// SPDX-License-Identifier: MIT

package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// SC is a google search client. Initialize it using NewSC.
type SC struct {
	// Authentication key.
	// https://developers.google.com/custom-search/v1/overview
	Key string
	// Search context engine identifier.
	// https://developers.google.com/custom-search/v1/cse/list
	Cx string
}

// NewSC returns a new google search client.
func NewSC(k, cx string) *SC {
	return &SC{
		Key: k,
		Cx:  cx,
	}
}

func (c *SC) Validate() error {
	switch {
	case c.Key == "":
		return fmt.Errorf("search client key missing")
	case c.Cx == "":
		return fmt.Errorf("search client cx missing")
	default:
		return nil
	}
}

const baseURL = "https://www.googleapis.com/customsearch/v1"

type Image struct {
	ByteSize    int    `json:"byteSize"`
	ContextLink string `json:"contextLink"`
	Height      int    `json:"height"`
	ThumbHeight int    `json:"thumbnailHeight"`
	ThumbLink   string `json:"thumbnailLink"`
	ThubmWidth  int    `json:"thumbnailWidth"`
	Width       int    `json:"width"`
}

type ISR struct {
	Image       *Image `json:"image"`
	Link        string `json:"link"`
	Mime        string `json:"mime"`
	Snippet     string `json:"snippet"`
	Title       string `json:"title"`
	DisplayLink string `json:"displayLink"`
}

func decodeISR(r io.Reader) ([]*ISR, error) {
	// Decode response.
	var res struct {
		Items []*ISR `json:"items"`
	}
	if err := json.NewDecoder(r).Decode(&res); err != nil {
		return nil, fmt.Errorf("unable to decode response: %w", err)
	}

	return res.Items, nil
}

func decodeError(r io.Reader) error {
	var res struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(r).Decode(&res); err != nil {
		return fmt.Errorf("unable to decode response: %w", err)
	}
	return fmt.Errorf(res.Error.Message)
}

const (
	ImgTypeClipart = "clipart"
	ImgTypeFace    = "face"
	ImgTypeLineart = "lineart"
	ImgTypeNews    = "news"
	ImgTypePhoto   = "photo"
)

func FilterImgType(s string) func(url.Values) {
	return func(v url.Values) {
		switch s {
		case "clipart", "face", "lineart", "news", "photo":
			v.Set("imgType", s)
		default:
			v.Del("imgType")
		}
	}
}

const (
	ImgSizeHuge      = "huge"
	ImgSizeIcon      = "icon"
	ImgSizeLarge     = "large"
	ImgSizeMedium    = "medium"
	ImgSizeSmall     = "small"
	ImgSizeXLarge    = "xlarge"
	ImgSizeXXLarge   = "xxlarge"
	ImgSizeUndefined = "undefined"
)

func FilterImgSize(s string) func(url.Values) {
	return func(v url.Values) {
		switch s {
		case "huge", "icon", "large", "medium", "small", "xlarge", "xxlarge":
			v.Set("imgSize", s)
		default:
			v.Del("imgSize")
		}
	}
}

var client = &http.Client{}

// SearchImages searches google for images.
func (c *SC) SearchImages(ctx context.Context, q string, opts ...func(url.Values)) ([]*ISR, error) {
	// Validate client
	if err := c.Validate(); err != nil {
		return nil, err
	}

	// Prepare URL.
	v := url.Values{}
	for _, f := range opts {
		f(v)
	}
	v.Set("key", c.Key)
	v.Set("cx", c.Cx)
	v.Set("searchType", "image")
	v.Set("q", q)
	v.Set("prettyPrint", "false")

	url, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse base url: %w", err)
	}
	url.RawQuery = v.Encode()

	// Prepare request
	req, err := http.NewRequestWithContext(ctx, "GET", url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("unable to build google search request: %w", err)
	}

	// Perform HTTP request.
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to contact google search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeError(resp.Body)
	}
	return decodeISR(resp.Body)
}
