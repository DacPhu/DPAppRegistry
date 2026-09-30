package appregistry

import (
	"errors"
	"net/http"
)

// DownloadTokenHeader carries the download token of a private app. The SDK sets it on update checks from
// CheckOptions.DownloadToken; an application that fetches the artifact itself must set it on that request too.
const DownloadTokenHeader = "X-Download-Token"

const maxRedirects = 10

// StripDownloadTokenOnRedirect is meant for http.Client.CheckRedirect when fetching an artifact URL.
//
// DPAppRegistry answers /download with a redirect to a presigned storage URL, and Go forwards custom headers
// across hosts (only Authorization, Cookie and WWW-Authenticate are dropped). Without this the download
// token would reach the storage provider and its access logs, where it has no business being.
func StripDownloadTokenOnRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("stopped after 10 redirects")
	}
	if len(via) > 0 && req.URL.Host != via[0].URL.Host {
		req.Header.Del(DownloadTokenHeader)
	}
	return nil
}
