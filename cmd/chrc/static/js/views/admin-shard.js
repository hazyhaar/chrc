var AdminShardView = (function () {
    var dossierID;

    function render(container, id) {
        dossierID = id;
        Dom.clear(container);

        container.appendChild(Dom.el('div', { class: 'page-header' }, [
            Dom.el('h1', { class: 'page-title' }, ['Dossier: ' + dossierID.slice(0, 12) + '...']),
            Dom.el('div', { class: 'page-actions' }, [
                Dom.el('button', { class: 'btn btn-ghost', onClick: function () { Router.navigate('admin'); } }, ['\u2190 Retour'])
            ])
        ]));

        var statsDiv = Dom.el('div', { id: 'shard-stats', style: 'margin-bottom: 24px;' });
        var sourcesDiv = Dom.el('div', { id: 'shard-sources', style: 'margin-bottom: 24px;' });
        var searchesDiv = Dom.el('div', { id: 'shard-searches', style: 'margin-bottom: 24px;' });
        var questionsDiv = Dom.el('div', { id: 'shard-questions' });

        container.appendChild(statsDiv);
        container.appendChild(sourcesDiv);
        container.appendChild(searchesDiv);
        container.appendChild(questionsDiv);

        loadStats();
        loadSources();
        loadSearches();
        loadQuestions();
    }

    function loadStats() {
        var div = document.getElementById('shard-stats');
        if (!div) return;
        Dom.clear(div);
        div.appendChild(Dom.el('h2', { style: 'margin-bottom: 12px;' }, ['Statistiques']));

        Api.get('/api/dossiers/' + dossierID + '/stats').then(function (stats) {
            if (!stats) return;
            var grid = Dom.el('div', { class: 'stats-grid' });
            [
                { label: 'Sources', value: stats.sources },
                { label: 'Extractions', value: stats.extractions },
                { label: 'Fetch logs', value: stats.fetch_logs }
            ].forEach(function (item) {
                grid.appendChild(Dom.el('div', { class: 'stat-card' }, [
                    Dom.el('div', { class: 'stat-label' }, [item.label]),
                    Dom.el('div', { class: 'stat-value' }, [String(item.value || 0)])
                ]));
            });
            div.appendChild(grid);
        });
    }

    function loadSources() {
        var div = document.getElementById('shard-sources');
        if (!div) return;
        Dom.clear(div);
        div.appendChild(Dom.el('h2', { style: 'margin-bottom: 12px;' }, ['Sources']));

        Api.get('/api/dossiers/' + dossierID + '/sources').then(function (sources) {
            if (!sources || sources.length === 0) {
                div.appendChild(Dom.el('div', { style: 'color: var(--text-muted);' }, ['Aucune source.']));
                return;
            }
            var table = Dom.el('table', { class: 'data-table' });
            table.appendChild(Dom.el('thead', {}, [
                Dom.el('tr', {}, [
                    Dom.el('th', {}, ['Nom']),
                    Dom.el('th', {}, ['Type']),
                    Dom.el('th', {}, ['Statut']),
                    Dom.el('th', {}, ['Derni\u00e8re collecte'])
                ])
            ]));
            var tbody = Dom.el('tbody');
            sources.forEach(function (src) {
                tbody.appendChild(Dom.el('tr', {}, [
                    Dom.el('td', {}, [src.name || src.url]),
                    Dom.el('td', {}, [typeBadge(src.source_type)]),
                    Dom.el('td', {}, [statusBadge(src.last_status)]),
                    Dom.el('td', { class: 'time-relative' }, [relTime(src.last_fetched_at)])
                ]));
            });
            table.appendChild(tbody);
            div.appendChild(table);
        });
    }

    function loadSearches() {
        var div = document.getElementById('shard-searches');
        if (!div) return;
        Dom.clear(div);
        div.appendChild(Dom.el('h2', { style: 'margin-bottom: 12px;' }, ['Recherches r\u00e9centes']));

        Api.get('/api/admin/overview/' + dossierID + '/searches?limit=30').then(function (entries) {
            if (!entries || entries.length === 0) {
                div.appendChild(Dom.el('div', { style: 'color: var(--text-muted);' }, ['Aucune recherche enregistr\u00e9e.']));
                return;
            }

            var table = Dom.el('table', { class: 'data-table' });
            table.appendChild(Dom.el('thead', {}, [
                Dom.el('tr', {}, [
                    Dom.el('th', {}, ['Requ\u00eate']),
                    Dom.el('th', {}, ['R\u00e9sultats']),
                    Dom.el('th', {}, ['Date']),
                    Dom.el('th', {}, ['Actions'])
                ])
            ]));

            var tbody = Dom.el('tbody');
            entries.forEach(function (e) {
                var row = Dom.el('tr', {}, [
                    Dom.el('td', {}, [e.query]),
                    Dom.el('td', {}, [String(e.result_count)]),
                    Dom.el('td', {}, [relTime(e.searched_at)]),
                    Dom.el('td', {}, [
                        Dom.el('button', {
                            class: 'btn btn-primary btn-sm',
                            onClick: function () { promoteSearch(e.query); }
                        }, ['Promouvoir'])
                    ])
                ]);
                tbody.appendChild(row);
            });
            table.appendChild(tbody);
            div.appendChild(table);
        });
    }

    function loadQuestions() {
        var div = document.getElementById('shard-questions');
        if (!div) return;
        Dom.clear(div);
        div.appendChild(Dom.el('h2', { style: 'margin-bottom: 12px;' }, ['Questions track\u00e9es']));

        Api.get('/api/dossiers/' + dossierID + '/questions').then(function (questions) {
            if (!questions || questions.length === 0) {
                div.appendChild(Dom.el('div', { style: 'color: var(--text-muted);' }, ['Aucune question.']));
                return;
            }
            questions.forEach(function (q) {
                div.appendChild(Dom.el('div', { class: 'card', style: 'margin-bottom: 8px; padding: 12px;' }, [
                    Dom.el('strong', {}, [q.text]),
                    Dom.el('div', { style: 'color: var(--text-muted); font-size: var(--font-size-sm); margin-top: 4px;' }, [
                        String(q.total_results || 0) + ' r\u00e9sultats \u2022 ',
                        q.enabled ? 'actif' : 'inactif'
                    ])
                ]));
            });
        });
    }

    function promoteSearch(query) {
        Modal.form('Promouvoir en question track\u00e9e', [
            { name: 'query', label: 'Requ\u00eate', type: 'text', value: query, required: true },
            { name: 'channels', label: 'Canaux (IDs moteurs, virgules)', type: 'text', value: 'brave_api' },
            { name: 'schedule_hours', label: 'Fr\u00e9quence (heures)', type: 'number', value: 24 }
        ], function (data) {
            var channels = (data.channels || 'brave_api').split(',').map(function (c) { return c.trim(); });
            Api.post('/api/admin/overview/' + dossierID + '/promote', {
                query: data.query,
                channels: channels,
                schedule_ms: Math.round((data.schedule_hours || 24) * 3600000)
            }).then(function () {
                Toast.success('Recherche promue en question track\u00e9e');
                loadSearches();
            });
        });
    }

    return { render: render };
})();
