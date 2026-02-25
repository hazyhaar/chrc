var Toast = (function () {
    function show(msg, type) {
        var container = document.getElementById('toast-container');
        if (!container) return;
        var toast = Dom.el('div', { class: 'toast toast-' + type }, [msg]);
        container.appendChild(toast);
        setTimeout(function () {
            toast.style.opacity = '0';
            toast.style.transition = 'opacity 0.2s ease';
            setTimeout(function () { if (toast.parentNode) toast.parentNode.removeChild(toast); }, 200);
        }, 4000);
    }

    function success(msg) { show(msg, 'success'); }
    function error(msg) { show(msg, 'error'); }
    function info(msg) { show(msg, 'info'); }

    return { success: success, error: error, info: info };
})();
