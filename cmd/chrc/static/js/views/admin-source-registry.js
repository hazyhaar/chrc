var AdminSourceRegistryView = (function () {
    function render(container) {
        Dom.clear(container);
        container.appendChild(Dom.el('div', { class: 'page-header' }, [
            Dom.el('h1', { class: 'page-title' }, ['Catalogue de sources']),
            Dom.el('div', { class: 'page-actions' }, [
                Dom.el('button', { class: 'btn btn-primary', onClick: showAddForm }, ['+ Ajouter'])
            ])
        ]));

        var tableWrap = Dom.el('div', { id: 'registry-table' });
        container.appendChild(tableWrap);
        loadRegistry();
    }

    function loadRegistry() {
        Api.get('/api/admin/source-registry').then(function (entries) {
            renderTable(entries || []);
        });
    }

    function renderTable(entries) {
        var wrap = document.getElementById('registry-table');
        if (!wrap) return;
        Dom.clear(wrap);

        if (entries.length === 0) {
            wrap.appendChild(Dom.el('div', { class: 'empty-state' }, [
                Dom.el('div', { class: 'empty-state-icon' }, ['\ud83d\udcda']),
                Dom.el('div', { class: 'empty-state-text' }, ['Catalogue vide.']),
                Dom.el('button', { class: 'btn btn-primary', onClick: showAddForm }, ['Ajouter une source'])
            ]));
            return;
        }

        // Group by category.
        var byCategory = {};
        entries.forEach(function (e) {
            var cat = e.category || 'sans cat\u00e9gorie';
            if (!byCategory[cat]) byCategory[cat] = [];
            byCategory[cat].push(e);
        });

        Object.keys(byCategory).sort().forEach(function (cat) {
            wrap.appendChild(Dom.el('h3', { style: 'margin-top: 20px; margin-bottom: 8px; text-transform: capitalize;' }, [cat]));

            var table = Dom.el('table', { class: 'data-table' });
            table.appendChild(Dom.el('thead', {}, [
                Dom.el('tr', {}, [
                    Dom.el('th', {}, ['Nom']),
                    Dom.el('th', {}, ['Type']),
                    Dom.el('th', {}, ['URL']),
                    Dom.el('th', {}, ['Intervalle']),
                    Dom.el('th', {}, ['Actif']),
                    Dom.el('th', {}, ['Actions'])
                ])
            ]));

            var tbody = Dom.el('tbody');
            byCategory[cat].forEach(function (entry) {
                var toggle = Dom.el('input', {
                    type: 'checkbox',
                    class: 'form-toggle',
                    onClick: function (ev) {
                        ev.stopPropagation();
                        Api.put('/api/admin/source-registry/' + entry.id, {
                            name: entry.name, url: entry.url, source_type: entry.source_type,
                            category: entry.category, config_json: entry.config_json,
                            description: entry.description, fetch_interval: entry.fetch_interval,
                            enabled: ev.target.checked
                        }).then(function () {
                            Toast.info(ev.target.checked ? 'Source activ\u00e9e' : 'Source d\u00e9sactiv\u00e9e');
                        });
                    }
                });
                if (entry.enabled) toggle.checked = true;

                var intervalMin = Math.round((entry.fetch_interval || 3600000) / 60000);
                var intervalLabel = intervalMin >= 1440 ? Math.round(intervalMin / 1440) + 'j' : intervalMin >= 60 ? Math.round(intervalMin / 60) + 'h' : intervalMin + 'min';

                var row = Dom.el('tr', {}, [
                    Dom.el('td', {}, [entry.name]),
                    Dom.el('td', {}, [typeBadge(entry.source_type)]),
                    Dom.el('td', { class: 'cell-url' }, [truncate(entry.url, 50)]),
                    Dom.el('td', {}, [intervalLabel]),
                    Dom.el('td', {}, [toggle]),
                    Dom.el('td', { class: 'cell-actions' }, [
                        Dom.el('button', { class: 'btn btn-ghost btn-sm', onClick: function () { showEditForm(entry); } }, ['\u270e']),
                        Dom.el('button', { class: 'btn btn-danger btn-sm', onClick: function () {
                            Modal.confirm('Supprimer cette source du catalogue ?', entry.name, function () {
                                Api.del('/api/admin/source-registry/' + entry.id).then(function () {
                                    Toast.success('Entr\u00e9e supprim\u00e9e');
                                    loadRegistry();
                                });
                            });
                        }}, ['\u2715'])
                    ])
                ]);
                tbody.appendChild(row);
            });
            table.appendChild(tbody);
            wrap.appendChild(table);
        });
    }

    function registryFields(entry) {
        return [
            { name: 'name', label: 'Nom', type: 'text', required: true, value: entry ? entry.name : '' },
            { name: 'url', label: 'URL', type: 'url', required: true, value: entry ? entry.url : '' },
            { name: 'source_type', label: 'Type', type: 'select', options: ['rss', 'web', 'api', 'document'], value: entry ? entry.source_type : 'rss' },
            { name: 'category', label: 'Cat\u00e9gorie', type: 'text', value: entry ? entry.category : '' },
            { name: 'fetch_interval_min', label: 'Intervalle (minutes)', type: 'number', value: entry ? Math.round((entry.fetch_interval || 3600000) / 60000) : 60 }
        ];
    }

    function showAddForm() {
        Modal.form('Nouvelle source', registryFields(null), function (data) {
            Api.post('/api/admin/source-registry', {
                name: data.name,
                url: data.url,
                source_type: data.source_type,
                category: data.category,
                fetch_interval: Math.round((data.fetch_interval_min || 60) * 60000),
                enabled: true
            }).then(function () {
                Toast.success('Source ajout\u00e9e au catalogue');
                loadRegistry();
            });
        });
    }

    function showEditForm(entry) {
        Modal.form('Modifier la source', registryFields(entry), function (data) {
            Api.put('/api/admin/source-registry/' + entry.id, {
                name: data.name,
                url: data.url,
                source_type: data.source_type,
                category: data.category,
                config_json: entry.config_json || '{}',
                description: entry.description || '',
                fetch_interval: Math.round((data.fetch_interval_min || 60) * 60000),
                enabled: entry.enabled
            }).then(function () {
                Toast.success('Source modifi\u00e9e');
                loadRegistry();
            });
        });
    }

    return { render: render };
})();
