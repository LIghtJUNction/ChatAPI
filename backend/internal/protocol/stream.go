package protocol

type StreamEvent struct {
	Event string
	Data  any
	Done  bool
}
