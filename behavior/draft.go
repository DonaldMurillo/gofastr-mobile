package behavior

import "context"

type DraftRevision struct {
	InstanceID string `json:"instance_id"`
	Revision   uint64 `json:"revision"`
	Token      string `json:"token"`
}

func (revision DraftRevision) Matches(snapshot Snapshot) bool {
	return revision.InstanceID != "" && revision.Token != "" && revision.InstanceID == snapshot.InstanceID && revision.Revision == snapshot.Revision && revision.Token == snapshot.DraftToken
}

func (snapshot Snapshot) DraftRevision() DraftRevision {
	return DraftRevision{InstanceID: snapshot.InstanceID, Revision: snapshot.Revision, Token: snapshot.DraftToken}
}

type DraftStore interface {
	Load(context.Context, string) (Snapshot, bool, error)
	Put(context.Context, string, Snapshot) error
	ClearIfRevision(context.Context, string, DraftRevision) (bool, error)
}
