package observer

import (
	"context"
	"time"

	"github.com/go-rod/rod/lib/proto"
	"github.com/hazyhaar/chrc/domwatch/mutation"
)

// cdpListener subscribes to CDP DOM events on a page and feeds records
// into the raw channel. It handles the key DOM events:
// childNodeInserted, childNodeRemoved, attributeModified, attributeRemoved,
// characterDataModified, documentUpdated.
type cdpListener struct {
	obs  *Observer
	ctx  context.Context
	stop context.CancelFunc
}

func newCDPListener(obs *Observer) *cdpListener {
	ctx, cancel := context.WithCancel(obs.ctx)
	return &cdpListener{obs: obs, ctx: ctx, stop: cancel}
}

func (cl *cdpListener) start() {
	// Enable the DOM domain to receive events.
	proto.DOMEnable{}.Call(cl.obs.tab.Page)

	go cl.listenAll()
}

// listenAll uses EachEvent to subscribe to all DOM events in a single goroutine.
func (cl *cdpListener) listenAll() {
	p := cl.obs.tab.Page

	wait := p.Context(cl.ctx).EachEvent(
		func(e *proto.DOMChildNodeInserted) {
			cl.obs.nodes.addNode(e.ParentNodeID, e.Node)
			xpath := cl.obs.nodes.getXPath(e.Node.NodeID)

			rec := mutation.Record{
				Op:       mutation.OpInsert,
				XPath:    xpath,
				NodeType: e.Node.NodeType,
				Tag:      e.Node.NodeName,
			}
			if e.Node.NodeValue != "" {
				rec.HTML = e.Node.NodeValue
			}

			cl.obs.rawCh <- rawRecord{record: rec, source: sourceCDP, at: time.Now()}
		},

		func(e *proto.DOMChildNodeRemoved) {
			xpath := cl.obs.nodes.getXPath(e.NodeID)
			cl.obs.nodes.removeNode(e.NodeID)

			cl.obs.rawCh <- rawRecord{
				record: mutation.Record{Op: mutation.OpRemove, XPath: xpath},
				source: sourceCDP,
				at:     time.Now(),
			}
		},

		func(e *proto.DOMAttributeModified) {
			xpath := cl.obs.nodes.getXPath(e.NodeID)
			cl.obs.rawCh <- rawRecord{
				record: mutation.Record{
					Op: mutation.OpAttr, XPath: xpath,
					Name: e.Name, Value: e.Value,
				},
				source: sourceCDP,
				at:     time.Now(),
			}
		},

		func(e *proto.DOMAttributeRemoved) {
			xpath := cl.obs.nodes.getXPath(e.NodeID)
			cl.obs.rawCh <- rawRecord{
				record: mutation.Record{Op: mutation.OpAttrDel, XPath: xpath, Name: e.Name},
				source: sourceCDP,
				at:     time.Now(),
			}
		},

		func(e *proto.DOMCharacterDataModified) {
			xpath := cl.obs.nodes.getXPath(e.NodeID)
			cl.obs.rawCh <- rawRecord{
				record: mutation.Record{Op: mutation.OpText, XPath: xpath, Value: e.CharacterData},
				source: sourceCDP,
				at:     time.Now(),
			}
		},

		func(e *proto.DOMDocumentUpdated) {
			cl.obs.rawCh <- rawRecord{
				record: mutation.Record{Op: mutation.OpDocReset},
				source: sourceCDP,
				at:     time.Now(),
			}
			select {
			case cl.obs.docResetCh <- struct{}{}:
			default:
			}
		},
	)

	// EachEvent returns a wait function that blocks until context is cancelled.
	wait()
}
