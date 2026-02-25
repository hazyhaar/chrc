var Api = (function () {
    function request(method, path, body) {
        var opts = {
            method: method,
            credentials: 'same-origin',
            headers: {}
        };
        if (body !== undefined) {
            opts.headers['Content-Type'] = 'application/json';
            opts.body = JSON.stringify(body);
        }
        EventBus.emit('loading:start');
        return fetch(path, opts)
            .then(function (resp) {
                EventBus.emit('loading:end');
                if (resp.status === 401) {
                    EventBus.emit('auth:failed');
                    return Promise.reject(new Error('Unauthorized'));
                }
                if (!resp.ok) {
                    return resp.json().catch(function () { return {}; }).then(function (data) {
                        var msg = data.error || ('HTTP ' + resp.status);
                        Toast.error(msg);
                        return Promise.reject(new Error(msg));
                    });
                }
                if (resp.status === 204) return null;
                return resp.json();
            })
            .catch(function (err) {
                EventBus.emit('loading:end');
                if (err.message !== 'Unauthorized') {
                    Toast.error(err.message);
                }
                return Promise.reject(err);
            });
    }

    function get(path) { return request('GET', path); }
    function post(path, body) { return request('POST', path, body); }
    function put(path, body) { return request('PUT', path, body); }
    function del(path) { return request('DELETE', path); }

    return { get: get, post: post, put: put, del: del };
})();
