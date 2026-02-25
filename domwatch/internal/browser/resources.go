// CLAUDE:SUMMARY Intercepts and blocks specified resource types (images, fonts, media, stylesheets) on Rod pages.
package browser

import (
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// applyResourceBlocking sets up request interception to block specified
// resource types (images, fonts, media, stylesheets).
func applyResourceBlocking(page *rod.Page, types []string) error {
	blockSet := make(map[string]bool, len(types))
	for _, t := range types {
		blockSet[strings.ToLower(t)] = true
	}

	router := page.HijackRequests()

	router.MustAdd("*", func(ctx *rod.Hijack) {
		resType := string(ctx.Request.Type())

		if shouldBlock(blockSet, resType) {
			ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
			return
		}
		ctx.ContinueRequest(&proto.FetchContinueRequest{})
	})

	go router.Run()

	return nil
}

func shouldBlock(blockSet map[string]bool, resType string) bool {
	lower := strings.ToLower(resType)

	// Map resource types to our config names.
	switch lower {
	case "image":
		return blockSet["images"]
	case "font":
		return blockSet["fonts"]
	case "media":
		return blockSet["media"]
	case "stylesheet":
		return blockSet["stylesheets"]
	}

	return blockSet[lower]
}
