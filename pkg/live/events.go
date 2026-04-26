// Package live implements the Live PR real-time review system.
//
// Event flow
//
//	Client ──WS──▶ ws_handler ──▶ hub.Publish ──▶ Redis channel
//	                                                   │
//	All WS/SSE clients ◀──fan-out────────────────────┘
//
// Protocol: every message (client→server and server→client) is a JSON object
// with at minimum a "type" string field.  The first message from the client
// after upgrade must be an AuthMessage; the connection is closed otherwise.
package live

// ─── Event type constants ───────────────────────────────────────────────────

const (
	// Client → Server
	EventAuth   = "auth"   // first message: send JWT token
	EventCursor = "cursor" // line highlight (ephemeral, not persisted)
	EventDraft  = "draft"  // inline comment draft (persisted)
	EventChat   = "chat"   // chat message (persisted)

	// Collective review — client sends these
	EventVerdictPropose = "verdict_propose" // propose a verdict for collective vote
	EventVerdictAccept  = "verdict_accept"  // accept the current proposal
	EventVerdictReject  = "verdict_reject"  // reject the current proposal

	// Server → Client
	EventAuthOK            = "auth_ok"             // auth accepted; includes session info
	EventAuthFail          = "auth_fail"           // auth rejected; connection closes
	EventPresence          = "presence"            // full participant list sent on join
	EventUserJoined        = "user_joined"         // a new participant joined
	EventUserLeft          = "user_left"           // a participant disconnected
	EventSessionClosed     = "session_closed"      // creator closed the lobby
	EventVerdictProposed   = "verdict_proposed"    // new proposal broadcast to all
	EventVerdictVoteUpdate = "verdict_vote_update" // acceptance tally update
	EventVerdictRejected   = "verdict_rejected"    // proposal was rejected
	EventVerdictFinalized  = "verdict_finalized"   // all accepted — review submitted

	// Sent once, right after auth_ok, so a late-joining participant can catch up.
	EventChatHistory = "chat_history" // full ordered chat log for this session
)

// ─── Incoming messages (Client → Server) ────────────────────────────────────

// AuthMessage is the mandatory first message after WebSocket upgrade.
type AuthMessage struct {
	Type  string `json:"type"`  // "auth"
	Token string `json:"token"` // JWT access token
}

// CursorMessage reports the user's current file+line position.
// Ephemeral — broadcast to all participants but not persisted.
type CursorMessage struct {
	Type string `json:"type"` // "cursor"
	File string `json:"file"` // relative file path
	Line int    `json:"line"` // 1-based line number
}

// DraftMessage carries an inline comment the user is composing.
// Persisted so latecomers can see existing drafts on join.
type DraftMessage struct {
	Type string `json:"type"` // "draft"
	File string `json:"file"`
	Line int    `json:"line"`
	Body string `json:"body"` // current draft text (empty = clear)
}

// ChatMessage is a lobby chat message.
type ChatMessage struct {
	Type     string `json:"type"` // "chat"
	Body     string `json:"body"`
	ParentID *uint  `json:"parentId,omitempty"` // optional reply-to message ID
}

// VerdictProposeMessage is submitted when a reviewer proposes a collective verdict.
// All other participants must accept before the review is created.
type VerdictProposeMessage struct {
	Type     string `json:"type"`     // "verdict_propose"
	Decision string `json:"decision"` // "approve" | "request_changes" | "comment"
	Body     string `json:"body"`     // optional review summary
}

// VerdictVoteMessage is sent to accept or reject the current proposal.
type VerdictVoteMessage struct {
	Type       string `json:"type"`       // "verdict_accept" | "verdict_reject"
	ProposalID string `json:"proposalId"` // ID of the proposal being voted on
}

// ─── Outgoing envelope (Server → Client) ────────────────────────────────────

// OutEvent is the envelope for every server-to-client message.
// It is JSON-marshalled before being written to the WebSocket or SSE stream.
type OutEvent struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

// ─── Payload structs ─────────────────────────────────────────────────────────

// UserInfo is the minimal public representation of a participant.
type UserInfo struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Avatar   string `json:"avatar,omitempty"`
}

// AuthOKPayload is sent immediately after a successful auth handshake.
type AuthOKPayload struct {
	SessionID uint     `json:"sessionId"`
	User      UserInfo `json:"user"`
}

// PresencePayload lists all users currently connected to the lobby.
// Sent once on join and after every join/leave event.
type PresencePayload struct {
	SessionID uint       `json:"sessionId"`
	Users     []UserInfo `json:"users"`
}

// UserJoinedPayload notifies existing participants of a new arrival.
type UserJoinedPayload struct {
	User UserInfo `json:"user"`
}

// UserLeftPayload notifies participants that someone has disconnected.
type UserLeftPayload struct {
	UserID uint `json:"userId"`
}

// CursorPayload is the server-side broadcast of a cursor position update.
type CursorPayload struct {
	UserID uint   `json:"userId"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

// DraftPayload is the server-side broadcast of a draft inline comment.
type DraftPayload struct {
	UserID uint   `json:"userId"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Body   string `json:"body"`
}

// ChatPayload is the server-side broadcast of a chat message.
type ChatPayload struct {
	ID        uint   `json:"id"`
	UserID    uint   `json:"userId"`
	Username  string `json:"username"`
	Body      string `json:"body"`
	ParentID  *uint  `json:"parentId,omitempty"` // set when this is a reply
	CreatedAt string `json:"createdAt"`          // RFC3339
}

// ChatHistoryPayload is sent once on join so late joiners can catch up.
type ChatHistoryPayload struct {
	Messages []ChatPayload `json:"messages"`
}

// VerdictProposedPayload is broadcast when a participant proposes a verdict.
type VerdictProposedPayload struct {
	ProposalID    string `json:"proposalId"`
	ProposerID    uint   `json:"proposerId"`
	ProposerName  string `json:"proposerName"`
	Decision      string `json:"decision"`
	Body          string `json:"body"`
	TotalNeeded   int    `json:"totalNeeded"`   // participants who must accept
	AcceptedCount int    `json:"acceptedCount"` // proposer auto-accepts (starts at 1)
}

// VerdictVoteUpdatePayload is broadcast each time a participant votes.
type VerdictVoteUpdatePayload struct {
	ProposalID    string `json:"proposalId"`
	UserID        uint   `json:"userId"`
	Username      string `json:"username"`
	AcceptedCount int    `json:"acceptedCount"`
	TotalNeeded   int    `json:"totalNeeded"`
}

// VerdictRejectedPayload is broadcast when any participant rejects the proposal.
type VerdictRejectedPayload struct {
	ProposalID string `json:"proposalId"`
	UserID     uint   `json:"userId"`
	Username   string `json:"username"`
}

// VerdictFinalizedPayload is broadcast once all participants have accepted.
// At this point the PRReview has already been persisted in the database.
type VerdictFinalizedPayload struct {
	ProposalID string `json:"proposalId"`
	Decision   string `json:"decision"`
	Body       string `json:"body"`
	ReviewID   uint   `json:"reviewId"` // the newly created PRReview.ID
}
