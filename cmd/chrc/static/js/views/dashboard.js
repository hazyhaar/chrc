var DashboardView = (function () {
    function render(container, spaceId) {
        Dom.clear(container);

        container.appendChild(Dom.el('div', { class: 'page-header' }, [
            Dom.el('h1', { class: 'page-title' }, ['Tableau de bord'])
        ]));

        var statsGrid = Dom.el('div', { class: 'stats-grid', id: 'dash-stats' });
        container.appendChild(statsGrid);

        var quickActions = Dom.el('div', { style: 'display: flex; gap: 12px; margin-bottom: 24px;' }, [
            Dom.el('button', { class: 'btn btn-primary', onClick: function () {
                Router.navigate(spaceId + '/sources');
            }}, ['Sources']),
            Dom.el('button', { class: 'btn', onClick: function () {
                Router.navigate(spaceId + '/questions');
            }}, ['Questions']),
            Dom.el('button', { class: 'btn', onClick: function () {
                Router.navigate(spaceId + '/search');
            }}, ['Rechercher'])
        ]);
        container.appendChild(quickActions);

        var recentSection = Dom.el('div', { class: 'detail-section' }, [
            Dom.el('h3', { class: 'detail-section-title' }, ['Sources r\u00e9centes'])
        ]);
        var recentTable = Dom.el('div', { id: 'dash-recent' });
        recentSection.appendChild(recentTable);
        container.appendChild(recentSection);

        loadStats(spaceId);
        loadRecentSources(spaceId);
    }

    function loadStats(spaceId) {
        Api.get('/api/dossiers/' + spaceId + '/stats').then(function (stats) {
            State.set('stats', stats);
            var grid = document.getElementById('dash-stats');
            if (!grid) return;
            Dom.clear(grid);

            var items = [
                { label: 'Sources', value: stats.sources },
                { label: 'Extractions', value: stats.extractions },
                { label: 'Fetch logs', value: stats.fetch_logs }
            ];

            items.forEach(function (item) {
                grid.appendChild(Dom.el('div', { class: 'stat-card' }, [
                    Dom.el('div', { class: 'stat-label' }, [item.label]),
                    Dom.el('div', { class: 'stat-value' }, [String(item.value || 0)])
                ]));
            });
        });
    }

    function loadRecentSources(spaceId) {
        Api.get('/api/dossiers/' + spaceId + '/sources').then(function (sources) {
            var el = document.getElementById('dash-recent');
            if (!el) return;
            Dom.clear(el);

            var recent = (sources || []).slice(0, 5);
            if (recent.length === 0) {
                el.appendChild(Dom.el('div', { class: 'empty-state' }, [
                    Dom.el('div', { class: 'empty-state-text' }, ['Aucune source.']),
                    Dom.el('button', { class: 'btn btn-primary', onClick: function () {
                        Router.navigate(spaceId + '/sources');
                    }}, ['Ajouter une source'])
                ]));
                return;
            }

            var table = Dom.el('table', { class: 'data-table' });
            var thead = Dom.el('thead', {}, [
                Dom.el('tr', {}, [
                    Dom.el('th', {}, ['Nom']),
                    Dom.el('th', {}, ['Type']),
                    Dom.el('th', {}, ['Statut']),
                    Dom.el('th', {}, ['Derni\u00e8re collecte'])
                ])
            ]);
            table.appendChild(thead);

            var tbody = Dom.el('tbody');
            recent.forEach(function (src) {
                var row = Dom.el('tr', { class: 'clickable', onClick: function () {
                    Router.navigate(spaceId + '/sources/' + src.id);
                }}, [
                    Dom.el('td', {}, [src.name || src.url]),
                    Dom.el('td', {}, [typeBadge(src.source_type)]),
                    Dom.el('td', {}, [statusBadge(src.last_status)]),
                    Dom.el('td', { class: 'time-relative' }, [relTime(src.last_fetched_at)])
                ]);
                tbody.appendChild(row);
            });
            table.appendChild(tbody);
            el.appendChild(table);
        });
    }

    return { render: render };
})();
