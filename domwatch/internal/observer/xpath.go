// CLAUDE:SUMMARY Tracks CDP node IDs to XPaths for mutation location reporting.
package observer

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// nodeMap tracks CDP nodeIDs to their XPaths.
type nodeMap struct {
	mu    sync.RWMutex
	paths map[proto.DOMNodeID]string
	// tags maps nodeID to tag name for sibling index computation.
	tags map[proto.DOMNodeID]string
	// parent maps nodeID to parent nodeID.
	parent map[proto.DOMNodeID]proto.DOMNodeID
	// children maps nodeID to ordered child nodeIDs.
	children map[proto.DOMNodeID][]proto.DOMNodeID
}

func newNodeMap() *nodeMap {
	return &nodeMap{
		paths:    make(map[proto.DOMNodeID]string),
		tags:     make(map[proto.DOMNodeID]string),
		parent:   make(map[proto.DOMNodeID]proto.DOMNodeID),
		children: make(map[proto.DOMNodeID][]proto.DOMNodeID),
	}
}

// buildFromDocument walks the full DOM tree returned by DOM.getDocument
// and populates the nodeMap.
func (nm *nodeMap) buildFromDocument(root *proto.DOMNode) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// Clear existing data.
	nm.paths = make(map[proto.DOMNodeID]string)
	nm.tags = make(map[proto.DOMNodeID]string)
	nm.parent = make(map[proto.DOMNodeID]proto.DOMNodeID)
	nm.children = make(map[proto.DOMNodeID][]proto.DOMNodeID)

	nm.walkNode(root, "")
}

func (nm *nodeMap) walkNode(node *proto.DOMNode, parentPath string) {
	if node == nil {
		return
	}

	xpath := nm.computeXPath(node, parentPath)
	nm.paths[node.NodeID] = xpath
	nm.tags[node.NodeID] = strings.ToLower(node.NodeName)

	for _, child := range node.Children {
		nm.parent[child.NodeID] = node.NodeID
		nm.children[node.NodeID] = append(nm.children[node.NodeID], child.NodeID)
		nm.walkNode(child, xpath)
	}

	// Shadow roots.
	if node.ShadowRoots != nil {
		for _, sr := range node.ShadowRoots {
			nm.parent[sr.NodeID] = node.NodeID
			nm.walkNode(sr, xpath+"/shadow-root")
		}
	}
}

func (nm *nodeMap) computeXPath(node *proto.DOMNode, parentPath string) string {
	name := strings.ToLower(node.NodeName)

	switch node.NodeType {
	case 9: // Document
		return ""
	case 10: // DocumentType
		return parentPath
	case 3: // Text
		return parentPath + "/text()"
	case 8: // Comment
		return parentPath + "/comment()"
	case 1: // Element
		// default: fall through
	default:
		return parentPath + "/" + name
	}

	if name == "html" {
		return "/html"
	}
	if name == "body" {
		return "/html/body"
	}
	if name == "head" {
		return "/html/head"
	}

	// Compute sibling index for the XPath.
	parentID, hasParent := nm.parent[node.NodeID]
	if !hasParent {
		return parentPath + "/" + name
	}

	siblings := nm.children[parentID]
	idx := 1
	for _, sibID := range siblings {
		if sibID == node.NodeID {
			break
		}
		if nm.tags[sibID] == name {
			idx++
		}
	}

	// Count total siblings with same tag.
	total := 0
	for _, sibID := range siblings {
		if nm.tags[sibID] == name {
			total++
		}
	}

	if total > 1 {
		return fmt.Sprintf("%s/%s[%d]", parentPath, name, idx)
	}
	return parentPath + "/" + name
}

// getXPath returns the cached XPath for a nodeID.
func (nm *nodeMap) getXPath(id proto.DOMNodeID) string {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	if p, ok := nm.paths[id]; ok {
		return p
	}
	return fmt.Sprintf("/unknown[nodeId=%d]", id)
}

// addNode registers a new node inserted into the DOM.
func (nm *nodeMap) addNode(parentID proto.DOMNodeID, node *proto.DOMNode) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	parentPath := nm.paths[parentID]
	nm.parent[node.NodeID] = parentID
	nm.children[parentID] = append(nm.children[parentID], node.NodeID)
	nm.walkNode(node, parentPath)
}

// removeNode removes a node from the map.
func (nm *nodeMap) removeNode(nodeID proto.DOMNodeID) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	nm.removeNodeLocked(nodeID)
}

func (nm *nodeMap) removeNodeLocked(nodeID proto.DOMNodeID) {
	// Remove children recursively.
	for _, childID := range nm.children[nodeID] {
		nm.removeNodeLocked(childID)
	}

	// Remove from parent's children list.
	if parentID, ok := nm.parent[nodeID]; ok {
		kids := nm.children[parentID]
		for i, id := range kids {
			if id == nodeID {
				nm.children[parentID] = append(kids[:i], kids[i+1:]...)
				break
			}
		}
	}

	delete(nm.paths, nodeID)
	delete(nm.tags, nodeID)
	delete(nm.parent, nodeID)
	delete(nm.children, nodeID)
}

// xpathFromPage evaluates an XPath for an element using JS in the page.
// Fallback for when the nodeMap doesn't have the node.
func xpathFromPage(page *rod.Page, selector string) string {
	res, err := page.Eval(fmt.Sprintf(`() => {
		const el = document.querySelector(%q);
		if (!el) return "";
		const parts = [];
		let node = el;
		while (node && node.nodeType === 1) {
			let idx = 0;
			let sibling = node.previousSibling;
			while (sibling) {
				if (sibling.nodeType === 1 && sibling.nodeName === node.nodeName) idx++;
				sibling = sibling.previousSibling;
			}
			const tag = node.nodeName.toLowerCase();
			parts.unshift(idx > 0 ? tag + "[" + (idx+1) + "]" : tag);
			node = node.parentNode;
		}
		return "/" + parts.join("/");
	}`, selector))
	if err != nil {
		return ""
	}
	return res.Value.Str()
}
