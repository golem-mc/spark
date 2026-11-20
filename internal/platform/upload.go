package platform

import (
	"context"
	"time"

	"github.com/golem-mc/spark/internal/text"
	"google.golang.org/protobuf/proto"
)

// uploadTimeout bounds one upload to bytebin.
const uploadTimeout = 30 * time.Second

// upload posts msg to bytebin and answers its viewer URL.
func (p *Platform) upload(msg proto.Message, mediaType, userAgentExtra string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()
	key, err := p.Bytebin.PostProto(ctx, msg, mediaType, userAgentExtra)
	if err != nil {
		return "", err
	}
	return p.ViewerURL + key, nil
}

// announceURL broadcasts a title and the clickable URL under it.
func announceURL(r *Responder, title, url string) {
	r.BroadcastPrefixed(text.T(title, text.Gold))
	r.Broadcast(text.OpenURL(text.T(url, text.Gray), url))
}
