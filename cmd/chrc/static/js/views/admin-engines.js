var AdminEnginesView = (function () {
    function render(container) {
        Dom.clear(container);
        container.appendChild(Dom.el('div', { class: 'page-header' }, [
            Dom.el('h1', { class: 'page-title' }, ['Moteurs de recherche']),
            Dom.el('div', { class: 'page-actions' }, [
                Dom.el('button', { class: 'btn btn-primary', onClick: showAddForm }, ['+ Ajouter'])
            ])
        ]));

        var tableWrap = Dom.el('div', { id: 'engines-table' });
        container.appendChild(tableWrap);
        loadEngines();
    }

    function loadEngines() {
        Api.get('/api/admin/engines').then(function (engines) {
            renderTable(engines || []);
        });
    }

    function renderTable(engines) {
        var wrap = document.getElementById('engines-table');
        if (!wrap) return;
        Dom.clear(wrap);

        if (engines.length === 0) {
            wrap.appendChild(Dom.el('div', { class: 'empty-state' }, [
                Dom.el('div', { class: 'empty-state-icon' }, ['\u2699']),
                Dom.el('div', { class: 'empty-state-text' }, ['Aucun moteur configur\u00e9.']),
                Dom.el('button', { class: 'btn btn-primary', onClick: showAddForm }, ['Ajouter un moteur'])
            ]));
            return;
        }

        var table = Dom.el('table', { class: 'data-table' });
        table.appendChild(Dom.el('thead', {}, [
            Dom.el('tr', {}, [
                Dom.el('th', {}, ['Nom']),
                Dom.el('th', {}, ['Strat\u00e9gie']),
                Dom.el('th', {}, ['URL']),
                Dom.el('th', {}, ['Rate limit']),
                Dom.el('th', {}, ['Pages max']),
                Dom.el('th', {}, ['Actif']),
                Dom.el('th', {}, ['Actions'])
            ])
        ]));

        var tbody = Dom.el('tbody');
        engines.forEach(function (eng) {
            var toggle = Dom.el('input', {
                type: 'checkbox',
                class: 'form-toggle',
                onClick: function (e) {
                    e.stopPropagation();
                    Api.put('/api/admin/engines/' + eng.id, {
                        name: eng.name, strategy: eng.strategy, url_template: eng.url_template,
                        api_config: eng.api_config, selectors: eng.selectors,
                        rate_limit_ms: eng.rate_limit_ms, max_pages: eng.max_pages,
                        enabled: e.target.checked
                    }).then(function () {
                        Toast.info(e.target.checked ? 'Moteur activ\u00e9' : 'Moteur d\u00e9sactiv\u00e9');
                    });
                }
            });
            if (eng.enabled) toggle.checked = true;

            var row = Dom.el('tr', {}, [
                Dom.el('td', {}, [eng.name]),
                Dom.el('td', {}, [typeBadge(eng.strategy)]),
                Dom.el('td', { class: 'cell-url' }, [truncate(eng.url_template, 50)]),
                Dom.el('td', {}, [String(eng.rate_limit_ms) + 'ms']),
                Dom.el('td', {}, [String(eng.max_pages)]),
                Dom.el('td', {}, [toggle]),
                Dom.el('td', { class: 'cell-actions' }, [
                    Dom.el('button', { class: 'btn btn-ghost btn-sm', onClick: function () { showEditForm(eng); } }, ['\u270e']),
                    Dom.el('button', { class: 'btn btn-danger btn-sm', onClick: function () {
                        Modal.confirm('Supprimer ce moteur ?', eng.name, function () {
                            Api.del('/api/admin/engines/' + eng.id).then(function () {
                                Toast.success('Moteur supprim\u00e9');
                                loadEngines();
                            });
                        });
                    }}, ['\u2715'])
                ])
            ]);
            tbody.appendChild(row);
        });
        table.appendChild(tbody);
        wrap.appendChild(table);
    }

    function engineFields(eng) {
        return [
            { name: 'name', label: 'Nom', type: 'text', required: true, value: eng ? eng.name : '' },
            { name: 'strategy', label: 'Strat\u00e9gie', type: 'select', options: ['api', 'generic'], value: eng ? eng.strategy : 'api' },
            { name: 'url_template', label: 'URL Template', type: 'text', required: true, value: eng ? eng.url_template : '', placeholder: 'https://api.example.com/search?q={query}' },
            { name: 'rate_limit_ms', label: 'Rate limit (ms)', type: 'number', value: eng ? eng.rate_limit_ms : 2000 },
            { name: 'max_pages', label: 'Pages max', type: 'number', value: eng ? eng.max_pages : 3 }
        ];
    }

    function showAddForm() {
        Modal.form('Nouveau moteur', engineFields(null), function (data) {
            Api.post('/api/admin/engines', {
                name: data.name,
                strategy: data.strategy,
                url_template: data.url_template,
                rate_limit_ms: Number(data.rate_limit_ms) || 2000,
                max_pages: Number(data.max_pages) || 3,
                enabled: true
            }).then(function () {
                Toast.success('Moteur ajout\u00e9');
                loadEngines();
            });
        });
    }

    function showEditForm(eng) {
        Modal.form('Modifier le moteur', engineFields(eng), function (data) {
            Api.put('/api/admin/engines/' + eng.id, {
                name: data.name,
                strategy: data.strategy,
                url_template: data.url_template,
                api_config: eng.api_config || '{}',
                selectors: eng.selectors || '{}',
                rate_limit_ms: Number(data.rate_limit_ms) || 2000,
                max_pages: Number(data.max_pages) || 3,
                enabled: eng.enabled
            }).then(function () {
                Toast.success('Moteur modifi\u00e9');
                loadEngines();
            });
        });
    }

    return { render: render };
})();
