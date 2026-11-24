package selfhost

import (
	"context"
	"crypto/sha256"
	"sync"
)

// Only digests and workspace pointers live in the index. Rotation, removal and duplicate
// detection use the same lock; a late rotation cannot reinsert a closed workspace.
type tokenIndex struct {
	mu       sync.RWMutex
	byDigest map[[sha256.Size]byte]map[*Workspace]struct{}
}

func (i *tokenIndex) replace(ws *Workspace, token string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.detach(ws)
	if token == "" || ws.ctx.Err() != nil {
		return
	}
	if i.byDigest == nil {
		i.byDigest = make(map[[sha256.Size]byte]map[*Workspace]struct{})
	}
	key := sha256.Sum256([]byte(token))
	if i.byDigest[key] == nil {
		i.byDigest[key] = make(map[*Workspace]struct{})
	}
	i.byDigest[key][ws] = struct{}{}
	ws.tokenDigest = key
	ws.tokenCtx, ws.endToken = context.WithCancel(ws.ctx)
}

func (i *tokenIndex) detach(ws *Workspace) {
	if ws.endToken != nil {
		ws.endToken()
		ws.endToken = nil
	}
	owners := i.byDigest[ws.tokenDigest]
	delete(owners, ws)
	if len(owners) == 0 {
		delete(i.byDigest, ws.tokenDigest)
	}
}

func (i *tokenIndex) lookup(token string) (*Workspace, context.Context) {
	key := sha256.Sum256([]byte(token))
	i.mu.RLock()
	defer i.mu.RUnlock()
	owners := i.byDigest[key]
	if len(owners) == 1 {
		for ws := range owners {
			return ws, ws.tokenCtx
		}
	}
	return nil, nil
}
