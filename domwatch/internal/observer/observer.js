// domwatch observer.js — injected MutationObserver + hooks.
// Zero dependencies. ~150 lines. Communicates with Go via Runtime.addBinding.
(function() {
  'use strict';

  if (window.__domwatcher_active) return;
  window.__domwatcher_active = true;

  var filters = (window.__domwatcher_filters || []).map(function(s) {
    try { return document.querySelector(s); } catch(e) { return null; }
  }).filter(Boolean);

  function shouldFilter(node) {
    for (var i = 0; i < filters.length; i++) {
      if (filters[i].contains(node)) return true;
    }
    return false;
  }

  function getXPath(node) {
    if (!node || node.nodeType === 9) return '';
    if (node.nodeType === 3) return getXPath(node.parentNode) + '/text()';
    if (node.nodeType === 8) return getXPath(node.parentNode) + '/comment()';
    if (node.nodeType !== 1) return '';

    var tag = node.nodeName.toLowerCase();
    if (tag === 'html') return '/html';
    if (tag === 'body') return '/html/body';
    if (tag === 'head') return '/html/head';

    var parent = node.parentNode;
    if (!parent) return '/' + tag;

    var siblings = parent.children;
    var sameTag = 0;
    var idx = 0;
    for (var i = 0; i < siblings.length; i++) {
      if (siblings[i].nodeName === node.nodeName) {
        sameTag++;
        if (siblings[i] === node) idx = sameTag;
      }
    }

    var step = sameTag > 1 ? tag + '[' + idx + ']' : tag;
    return getXPath(parent) + '/' + step;
  }

  function send(records) {
    if (typeof window.__domwatcher_binding === 'function') {
      window.__domwatcher_binding(JSON.stringify(records));
    }
  }

  var observer = new MutationObserver(function(mutations) {
    var records = [];
    for (var i = 0; i < mutations.length; i++) {
      var m = mutations[i];
      var target = m.target;
      if (shouldFilter(target)) continue;

      switch (m.type) {
        case 'childList':
          for (var j = 0; j < m.addedNodes.length; j++) {
            var added = m.addedNodes[j];
            if (shouldFilter(added)) continue;
            records.push({
              op: 'insert',
              xpath: getXPath(added),
              node_type: added.nodeType,
              tag: added.nodeName ? added.nodeName.toLowerCase() : '',
              html: added.nodeType === 1 ? added.outerHTML : (added.textContent || '')
            });
          }
          for (var k = 0; k < m.removedNodes.length; k++) {
            var removed = m.removedNodes[k];
            records.push({
              op: 'remove',
              xpath: getXPath(target) + '/*[removed]',
              node_type: removed.nodeType,
              tag: removed.nodeName ? removed.nodeName.toLowerCase() : ''
            });
          }
          break;

        case 'attributes':
          records.push({
            op: 'attr',
            xpath: getXPath(target),
            name: m.attributeName,
            value: target.getAttribute(m.attributeName) || '',
            old_value: m.oldValue || ''
          });
          break;

        case 'characterData':
          records.push({
            op: 'text',
            xpath: getXPath(target),
            value: target.textContent || '',
            old_value: m.oldValue || ''
          });
          break;
      }
    }

    if (records.length > 0) send(records);
  });

  observer.observe(document.documentElement, {
    childList: true,
    attributes: true,
    characterData: true,
    subtree: true,
    attributeOldValue: true,
    characterDataOldValue: true
  });

  // Hook History.pushState and replaceState for SPA navigation detection.
  var origPush = History.prototype.pushState;
  var origReplace = History.prototype.replaceState;

  History.prototype.pushState = function() {
    var result = origPush.apply(this, arguments);
    if (typeof window.__domwatcher_binding === 'function') {
      window.__domwatcher_binding(JSON.stringify([{op: '__navigate', value: location.href}]));
    }
    return result;
  };

  History.prototype.replaceState = function() {
    var result = origReplace.apply(this, arguments);
    if (typeof window.__domwatcher_binding === 'function') {
      window.__domwatcher_binding(JSON.stringify([{op: '__navigate', value: location.href}]));
    }
    return result;
  };

  window.addEventListener('popstate', function() {
    if (typeof window.__domwatcher_binding === 'function') {
      window.__domwatcher_binding(JSON.stringify([{op: '__navigate', value: location.href}]));
    }
  });

  // Hook Element.attachShadow for shadow root discovery.
  var origAttachShadow = Element.prototype.attachShadow;
  Element.prototype.attachShadow = function(init) {
    var shadow = origAttachShadow.apply(this, arguments);
    if (init.mode === 'open') {
      if (typeof window.__domwatcher_binding === 'function') {
        window.__domwatcher_binding(JSON.stringify([{
          op: '__shadow',
          xpath: getXPath(this),
          value: 'open'
        }]));
      }
      // Observe the shadow root too.
      observer.observe(shadow, {
        childList: true,
        attributes: true,
        characterData: true,
        subtree: true,
        attributeOldValue: true,
        characterDataOldValue: true
      });
    }
    return shadow;
  };
})();
