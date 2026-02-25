var State = (function () {
    var data = {
        spaces: [],
        currentSpaceId: null,
        sources: [],
        questions: [],
        stats: null
    };

    function get(key) {
        return data[key];
    }

    function set(key, value) {
        data[key] = value;
        EventBus.emit('state:' + key, value);
    }

    return { get: get, set: set };
})();
