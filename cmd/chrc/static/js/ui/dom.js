var Dom = (function () {
    function el(tag, attrs, children) {
        var node = document.createElement(tag);
        if (attrs) {
            Object.keys(attrs).forEach(function (k) {
                if (k === 'class') node.className = attrs[k];
                else if (k === 'dataset') {
                    Object.keys(attrs[k]).forEach(function (d) { node.dataset[d] = attrs[k][d]; });
                } else if (k.indexOf('on') === 0) {
                    node.addEventListener(k.slice(2).toLowerCase(), attrs[k]);
                } else node.setAttribute(k, attrs[k]);
            });
        }
        if (children) {
            if (!Array.isArray(children)) children = [children];
            children.forEach(function (c) {
                if (typeof c === 'string') node.appendChild(document.createTextNode(c));
                else if (c) node.appendChild(c);
            });
        }
        return node;
    }

    function text(str) {
        return document.createTextNode(str);
    }

    function clear(parent) {
        while (parent.firstChild) parent.removeChild(parent.firstChild);
    }

    function on(node, event, fn) {
        node.addEventListener(event, fn);
    }

    return { el: el, text: text, clear: clear, on: on };
})();
