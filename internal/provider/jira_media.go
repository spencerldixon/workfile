package provider

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
)

const maxJiraMediaCandidates = 8

var jiraMediaUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var jiraMediaPath = regexp.MustCompile(`(?i)(?:^|/)file/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:/|$)`)

// A media locator is opaque above this adapter. It keeps unnamed ADF images
// resolvable without requesting metadata until the UI actually wants an image.
func jiraMediaReference(media string, files []jiraFile) string {
	if !jiraMediaUUID.MatchString(media) {
		return ""
	}
	var ids []string
	for _, file := range files {
		if !attachmentID.MatchString(file.id) || !file.isImage() {
			continue
		}
		ids = append(ids, file.id)
		if len(ids) == maxJiraMediaCandidates {
			break
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return "jira-media:" + strings.ToLower(media) + ":" + strings.Join(ids, ",")
}

func (f jiraFile) isImage() bool {
	if f.mimeType != "" {
		return strings.HasPrefix(strings.ToLower(f.mimeType), "image/")
	}
	switch strings.ToLower(path.Ext(f.name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".avif", ".tif", ".tiff", ".svg":
		return true
	}
	return false
}

func parseJiraMediaReference(ref string) (media string, ids []string, err error) {
	parts := strings.Split(ref, ":")
	if len(parts) != 3 || parts[0] != "jira-media" || !jiraMediaUUID.MatchString(parts[1]) {
		return "", nil, errors.New("not an attachment id")
	}
	ids = strings.Split(parts[2], ",")
	if len(ids) > maxJiraMediaCandidates {
		return "", nil, errors.New("invalid media attachment reference")
	}
	for _, id := range ids {
		if !attachmentID.MatchString(id) {
			return "", nil, errors.New("invalid media attachment reference")
		}
	}
	return strings.ToLower(parts[1]), ids, nil
}

type jiraMediaIdentity struct {
	once  sync.Once
	media string
	err   error
}

func (c jiraTracker) mediaIdentity(ctx context.Context, base, auth, id string) (string, error) {
	key := c.name + "/" + id
	value, _ := c.jiraMedia.LoadOrStore(key, &jiraMediaIdentity{})
	cached := value.(*jiraMediaIdentity)
	cached.once.Do(func() { cached.media, cached.err = c.readMediaIdentity(ctx, base, auth, id) })
	if cached.err != nil {
		c.jiraMedia.CompareAndDelete(key, cached)
	}
	return cached.media, cached.err
}

func (c jiraTracker) readMediaIdentity(ctx context.Context, base, auth, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", base+"/rest/api/3/attachment/content/"+id, nil)
	if err != nil {
		return "", errors.New("invalid provider URL")
	}
	req.Header.Set("User-Agent", "workfile")
	req.Header.Set("Authorization", auth)
	// The redirect is identity metadata only. Never visit its target or forward
	// credentials, even when a caller supplies a custom HTTP client.
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("cannot read image attachment metadata")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		location, err := url.Parse(response.Header.Get("Location"))
		if err != nil {
			return "", errors.New("image attachment metadata is unavailable")
		}
		match := jiraMediaPath.FindStringSubmatch(location.Path)
		if len(match) == 2 {
			return strings.ToLower(match[1]), nil
		}
		return "", errors.New("image attachment metadata is unavailable")
	default:
		if response.StatusCode >= 400 {
			return "", errors.New("cannot read image attachment metadata")
		}
		return "", errors.New("image attachment metadata is unavailable")
	}
}

func (c jiraTracker) mediaAttachment(ctx context.Context, base, auth, media string, ids []string) (string, error) {
	for _, id := range ids {
		identity, err := c.mediaIdentity(ctx, base, auth, id)
		if err != nil {
			return "", err
		}
		if identity == media {
			return id, nil
		}
	}
	return "", errors.New("image attachment could not be matched")
}
