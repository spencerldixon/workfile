package workfile

import "time"

// BlockKind says how a block of a ticket's text is laid out.
type BlockKind string

const (
	BlockHeading   BlockKind = "heading"
	BlockParagraph BlockKind = "paragraph"
	BlockList      BlockKind = "list"
	BlockCode      BlockKind = "code"
	BlockTable     BlockKind = "table"
	BlockRule      BlockKind = "rule"
	BlockImage     BlockKind = "image"
)

// Span is a run of text in one style.
type Span struct {
	Text               string
	Bold, Italic, Code bool
	Link               string
}

// Block is one piece of a ticket's rich text, whichever tracker wrote it. Lists
// are flat: Level says how deep an item sits, and Marker is its bullet or number.
type Block struct {
	Kind   BlockKind
	Level  int
	Marker string
	Quoted bool
	Spans  []Span
	Rows   [][]string // BlockTable: the first row is the header when Header is set
	Header bool
	// BlockImage: the file's name, and what the tracker needs to fetch it. Only
	// the tracker reads Attachment. An image without one can only be named.
	Name, Attachment string
}

// Comment is one comment on a ticket. Created is zero when it is not known.
// Replies stay empty on a tracker whose comments are not threaded.
type Comment struct {
	Author  string
	Created time.Time
	Body    []Block
	Replies []Comment
}

// Detail is the text of one ticket, as its tracker shows it.
type Detail struct {
	Key  string
	Body []Block
	// CommentTotal is how many comments exist. A tracker may return fewer.
	Comments     []Comment
	CommentTotal int
}

// Listing is the little a ticket list needs, read for every ticket in scope.
// Status is the tracker's own word and State is the policy's. State is empty
// when the status is not mapped. Assignee is a name to show, and AssigneeID is
// the identity to compare. Type and both assignee fields may be empty.
type Listing struct {
	Key, Title, Status, State, Assignee, AssigneeID, Type, URL string
}
