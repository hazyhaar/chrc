var Modal = (function () {
    function close() {
        var overlay = document.querySelector('.modal-overlay');
        if (overlay) overlay.parentNode.removeChild(overlay);
    }

    function show(title, bodyEl, buttons) {
        close();
        var header = Dom.el('div', { class: 'modal-header' }, [
            Dom.el('div', { class: 'modal-title' }, [title]),
            Dom.el('button', { class: 'btn btn-ghost btn-sm', onClick: close }, ['\u00d7'])
        ]);
        var footer = Dom.el('div', { class: 'modal-footer' }, buttons);
        var box = Dom.el('div', { class: 'modal-box' }, [
            header,
            Dom.el('div', { class: 'modal-body' }, [bodyEl]),
            footer
        ]);
        var overlay = Dom.el('div', { class: 'modal-overlay', onClick: function (e) {
            if (e.target === overlay) close();
        }}, [box]);
        document.body.appendChild(overlay);
    }

    function confirm(title, message, onConfirm) {
        var body = Dom.el('p', {}, [message]);
        show(title, body, [
            Dom.el('button', { class: 'btn', onClick: close }, ['Annuler']),
            Dom.el('button', { class: 'btn btn-danger', onClick: function () { close(); onConfirm(); } }, ['Supprimer'])
        ]);
    }

    function form(title, fields, onSubmit) {
        var formEl = Dom.el('form', {});
        fields.forEach(function (f) {
            var group = Dom.el('div', { class: 'form-group' });
            group.appendChild(Dom.el('label', { class: 'form-label', for: f.name }, [f.label]));

            var input;
            if (f.type === 'select') {
                input = Dom.el('select', { class: 'form-select', name: f.name, id: f.name });
                (f.options || []).forEach(function (opt) {
                    var o = Dom.el('option', { value: opt }, [opt]);
                    if (f.value === opt) o.selected = true;
                    input.appendChild(o);
                });
            } else if (f.type === 'textarea') {
                input = Dom.el('textarea', { class: 'form-textarea', name: f.name, id: f.name });
                if (f.value) input.textContent = f.value;
            } else if (f.type === 'checkbox') {
                var row = Dom.el('div', { class: 'form-checkbox-row' });
                input = Dom.el('input', { type: 'checkbox', name: f.name, id: f.name });
                if (f.value) input.checked = true;
                row.appendChild(input);
                row.appendChild(Dom.el('span', {}, [f.label]));
                group.removeChild(group.firstChild); // remove label
                group.appendChild(row);
                formEl.appendChild(group);
                return;
            } else {
                input = Dom.el('input', {
                    class: 'form-input',
                    type: f.type || 'text',
                    name: f.name,
                    id: f.name,
                    placeholder: f.placeholder || ''
                });
                if (f.value !== undefined) input.value = f.value;
                if (f.required) input.required = true;
            }
            group.appendChild(input);
            formEl.appendChild(group);
        });

        show(title, formEl, [
            Dom.el('button', { class: 'btn', type: 'button', onClick: close }, ['Annuler']),
            Dom.el('button', { class: 'btn btn-primary', type: 'button', onClick: function () {
                var data = {};
                fields.forEach(function (f) {
                    var el = formEl.querySelector('[name="' + f.name + '"]');
                    if (!el) return;
                    if (f.type === 'checkbox') data[f.name] = el.checked;
                    else if (f.type === 'number') data[f.name] = parseFloat(el.value) || 0;
                    else data[f.name] = el.value;
                });
                close();
                onSubmit(data);
            }}, ['Valider'])
        ]);
    }

    return { show: show, confirm: confirm, form: form, close: close };
})();
