var SourcesView = (function () {
    var currentSpaceId = null;

    function render(container, spaceId) {
        currentSpaceId = spaceId;
        Dom.clear(container);

        container.appendChild(Dom.el('div', { class: 'page-header' }, [
            Dom.el('h1', { class: 'page-title' }, ['Sources']),
            Dom.el('div', { class: 'page-actions' }, [
                Dom.el('button', { class: 'btn btn-ghost', onClick: function () { showRegistryPicker(spaceId); } }, ['Depuis le catalogue']),
                Dom.el('button', { class: 'btn btn-primary', onClick: showAddForm }, ['+ Ajouter'])
            ])
        ]));

        var tableWrap = Dom.el('div', { id: 'sources-table' });
        container.appendChild(tableWrap);

        loadSources(spaceId);
    }

    function loadSources(spaceId) {
        Api.get('/api/dossiers/' + spaceId + '/sources').then(function (sources) {
            State.set('sources', sources || []);
            renderTable(sources || []);
        });
    }

    function renderTable(sources) {
        var wrap = document.getElementById('sources-table');
        if (!wrap) return;
        Dom.clear(wrap);

        if (sources.length === 0) {
            wrap.appendChild(Dom.el('div', { class: 'empty-state' }, [
                Dom.el('div', { class: 'empty-state-icon' }, ['\u{1f310}']),
                Dom.el('div', { class: 'empty-state-text' }, ['Aucune source. Ajoutez des URLs \u00e0 surveiller.']),
                Dom.el('button', { class: 'btn btn-primary', onClick: showAddForm }, ['Ajouter une source'])
            ]));
            return;
        }

        var table = Dom.el('table', { class: 'data-table' });
        table.appendChild(Dom.el('thead', {}, [
            Dom.el('tr', {}, [
                Dom.el('th', {}, ['Nom']),
                Dom.el('th', {}, ['Type']),
                Dom.el('th', {}, ['URL']),
                Dom.el('th', {}, ['Statut']),
                Dom.el('th', {}, ['Derni\u00e8re collecte']),
                Dom.el('th', {}, ['Erreurs']),
                Dom.el('th', {}, ['Actif']),
                Dom.el('th', {}, ['Actions'])
            ])
        ]));

        var tbody = Dom.el('tbody');
        sources.forEach(function (src) {
            var toggle = Dom.el('input', {
                type: 'checkbox',
                class: 'form-toggle',
                onClick: function (e) {
                    e.stopPropagation();
                    Api.put('/api/dossiers/' + currentSpaceId + '/sources/' + src.id,
                        { enabled: e.target.checked }).then(function () {
                        Toast.info(e.target.checked ? 'Source activ\u00e9e' : 'Source d\u00e9sactiv\u00e9e');
                    });
                }
            });
            if (src.enabled) toggle.checked = true;

            var row = Dom.el('tr', { class: 'clickable', onClick: function () {
                Router.navigate(currentSpaceId + '/sources/' + src.id);
            }}, [
                Dom.el('td', {}, [src.name || '-']),
                Dom.el('td', {}, [typeBadge(src.source_type)]),
                Dom.el('td', { class: 'cell-url' }, [src.url || '']),
                Dom.el('td', {}, [statusBadge(src.last_status)]),
                Dom.el('td', { class: 'time-relative' }, [relTime(src.last_fetched_at)]),
                Dom.el('td', {}, [src.fail_count > 0
                    ? Dom.el('span', { style: 'color: var(--danger)' }, [String(src.fail_count)])
                    : Dom.text('-')]),
                Dom.el('td', {}, [toggle]),
                Dom.el('td', { class: 'cell-actions' }, [
                    Dom.el('button', { class: 'btn btn-ghost btn-sm', title: 'Fetch Now', onClick: function (e) {
                        e.stopPropagation();
                        Api.post('/api/dossiers/' + currentSpaceId + '/sources/' + src.id + '/fetch').then(function () {
                            Toast.success('Fetch lanc\u00e9');
                            loadSources(currentSpaceId);
                        });
                    }}, ['\u{21bb}']),
                    Dom.el('button', { class: 'btn btn-ghost btn-sm', title: 'Modifier', onClick: function (e) {
                        e.stopPropagation();
                        showEditForm(src);
                    }}, ['\u{270e}']),
                    Dom.el('button', { class: 'btn btn-danger btn-sm', title: 'Supprimer', onClick: function (e) {
                        e.stopPropagation();
                        Modal.confirm('Supprimer cette source ?',
                            'Les extractions associ\u00e9es seront supprim\u00e9es.',
                            function () {
                                Api.del('/api/dossiers/' + currentSpaceId + '/sources/' + src.id).then(function () {
                                    Toast.success('Source supprim\u00e9e');
                                    loadSources(currentSpaceId);
                                });
                            });
                    }}, ['\u{2715}'])
                ])
            ]);
            tbody.appendChild(row);
        });
        table.appendChild(tbody);
        wrap.appendChild(table);
    }

    function sourceFields(src) {
        return [
            { name: 'name', label: 'Nom', type: 'text', required: true, value: src ? src.name : '' },
            { name: 'url', label: 'URL', type: 'url', required: true, value: src ? src.url : '', placeholder: 'https://...' },
            { name: 'source_type', label: 'Type', type: 'select', options: ['web', 'rss', 'api', 'document'], value: src ? src.source_type : 'web' },
            { name: 'fetch_interval', label: 'Intervalle (minutes)', type: 'number', value: src ? Math.round((src.fetch_interval || 3600000) / 60000) : 60 }
        ];
    }

    function showAddForm() {
        Modal.form('Nouvelle source', sourceFields(null), function (data) {
            Api.post('/api/dossiers/' + currentSpaceId + '/sources', {
                name: data.name,
                url: data.url,
                source_type: data.source_type,
                fetch_interval: Math.round(data.fetch_interval * 60000)
            }).then(function () {
                Toast.success('Source ajout\u00e9e');
                loadSources(currentSpaceId);
            });
        });
    }

    function showEditForm(src) {
        Modal.form('Modifier la source', sourceFields(src), function (data) {
            Api.put('/api/dossiers/' + currentSpaceId + '/sources/' + src.id, {
                name: data.name,
                url: data.url,
                fetch_interval: Math.round(data.fetch_interval * 60000)
            }).then(function () {
                Toast.success('Source modifi\u00e9e');
                loadSources(currentSpaceId);
            });
        });
    }

    function showRegistryPicker(spaceId) {
        Api.get('/api/source-registry').then(function (entries) {
            if (!entries || entries.length === 0) {
                Toast.info('Catalogue vide');
                return;
            }
            var content = Dom.el('div', { style: 'max-height: 400px; overflow-y: auto;' });
            var grouped = {};
            entries.forEach(function (e) {
                if (!e.enabled) return;
                var cat = e.category || 'autre';
                if (!grouped[cat]) grouped[cat] = [];
                grouped[cat].push(e);
            });

            Object.keys(grouped).sort().forEach(function (cat) {
                content.appendChild(Dom.el('h4', { style: 'margin: 12px 0 6px; text-transform: capitalize;' }, [cat]));
                grouped[cat].forEach(function (entry) {
                    content.appendChild(Dom.el('div', {
                        style: 'display: flex; justify-content: space-between; align-items: center; padding: 6px 0; border-bottom: 1px solid var(--border);'
                    }, [
                        Dom.el('div', {}, [
                            Dom.el('strong', {}, [entry.name]),
                            Dom.el('div', { style: 'font-size: var(--font-size-sm); color: var(--text-muted);' }, [truncate(entry.url, 60)])
                        ]),
                        Dom.el('button', {
                            class: 'btn btn-primary btn-sm',
                            onClick: function () {
                                Api.post('/api/dossiers/' + spaceId + '/sources/from-registry/' + entry.id).then(function () {
                                    Toast.success(entry.name + ' ajout\u00e9e');
                                    loadSources(spaceId);
                                });
                            }
                        }, ['Ajouter'])
                    ]));
                });
            });

            Modal.show('Catalogue de sources', content);
        });
    }

    return { render: render };
})();
