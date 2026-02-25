var Router = (function () {
    function parse(hash) {
        var parts = (hash || '').replace(/^#\/?/, '').split('/').filter(Boolean);

        // Detect admin prefix: #/admin, #/admin/engines, #/admin/overview/uid/sid
        if (parts[0] === 'admin') {
            return {
                admin: true,
                spaceId: null,
                view: parts[1] || null,
                itemId: parts[2] || null,
                extra: parts[3] || null
            };
        }

        return {
            admin: false,
            spaceId: parts[0] || null,
            view: parts[1] || null,
            itemId: parts[2] || null,
            extra: null
        };
    }

    function current() {
        return parse(window.location.hash);
    }

    function navigate(path) {
        window.location.hash = '#/' + path;
    }

    function init() {
        function onRoute() {
            var route = current();
            EventBus.emit('route:change', route);
        }
        window.addEventListener('hashchange', onRoute);
        onRoute();
    }

    return { init: init, navigate: navigate, current: current };
})();
