// SPDX-FileCopyrightText: 2020 Jecoz
//
// SPDX-License-Identifier: MIT

package google

import (
	"strings"
	"testing"
)

var gsiResponse = `{
    "context": {
        "title": "discoursive-engine"
    },
    "items": [
        {
            "displayLink": "www.wired.com",
            "fileFormat": "image/jpeg",
            "htmlSnippet": "<b>Cats</b> Are Making Australia&#39;s Bushfire Tragedy Even Worse | WIRED",
            "htmlTitle": "<b>Cats</b> Are Making Australia&#39;s Bushfire Tragedy Even Worse | WIRED",
            "image": {
                "byteSize": 1063166,
                "contextLink": "https://www.wired.com/story/cats-australia-bushfires/",
                "height": 1532,
                "thumbnailHeight": 112,
                "thumbnailLink": "https://encrypted-tbn0.gstatic.com/images?q=tbn:ANd9GcRqvzpzGSM6rfah2XAYcVkrQaXPUmZY6_RYXLBHG67zAVNp8lHpL0WCQR49&s",
                "thumbnailWidth": 150,
                "width": 2043
            },
            "kind": "customsearch#result",
            "link": "https://media.wired.com/photos/5e1e646743940d0008009167/4:3/w_2043,h_1532,c_limit/Science_Cats-84873657.jpg",
            "mime": "image/jpeg",
            "snippet": "Cats Are Making Australia's Bushfire Tragedy Even Worse | WIRED",
            "title": "Cats Are Making Australia's Bushfire Tragedy Even Worse | WIRED"
        }
    ],
    "kind": "customsearch#search",
    "queries": {
        "nextPage": [
            {
                "count": 10,
                "cx": "017323123285766759920:bmy8nbrc5kp",
                "inputEncoding": "utf8",
                "outputEncoding": "utf8",
                "safe": "off",
                "searchTerms": "cats",
                "searchType": "image",
                "startIndex": 11,
                "title": "Google Custom Search - cats",
                "totalResults": "7410000000"
            }
        ],
        "request": [
            {
                "count": 10,
                "cx": "017323123285766759920:bmy8nbrc5kp",
                "inputEncoding": "utf8",
                "outputEncoding": "utf8",
                "safe": "off",
                "searchTerms": "cats",
                "searchType": "image",
                "startIndex": 1,
                "title": "Google Custom Search - cats",
                "totalResults": "7410000000"
            }
        ]
    },
    "searchInformation": {
        "formattedSearchTime": "0.61",
        "formattedTotalResults": "7,410,000,000",
        "searchTime": 0.610091,
        "totalResults": "7410000000"
    }
}
`

func TestDecodeISR(t *testing.T) {
	r := strings.NewReader(gsiResponse)
	items, err := decodeISR(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected items count: %d", len(items))
	}
}
