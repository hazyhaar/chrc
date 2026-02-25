var AdminOverviewView = (function () {
    function render(container) {
        Dom.clear(container);
        container.appendChild(Dom.el('div', { class: 'page-header' }, [
            Dom.el('h1', { class: 'page-title' }, ['Vue d\'ensemble'])
        ]));

        var grid = Dom.el('div', { id: 'overview-grid', style: 'display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 16px;' });
        container.appendChild(grid);

        Api.get('/api/admin/overview').then(function (data) {
            renderOverview(grid, data);
        });
    }

    function renderOverview(grid, data) {
        Dom.clear(grid);

        if (!data.shards || data.shards.length === 0) {
            grid.appendChild(Dom.el('div', { class: 'empty-state' }, [
                Dom.el('div', { class: 'empty-state-icon' }, ['\u25a3']),
                Dom.el('div', { class: 'empty-state-text' }, ['Aucun espace actif.'])
            ]));
            return;
        }

        // Build user lookup.
        var userMap = {};
        (data.users || []).forEach(function (u) { userMap[u.id] = u; });

        data.shards.forEach(function (shard) {
            var stats = shard.stats || {};

            var card = Dom.el('div', {
                class: 'card clickable',
                style: 'padding: 16px; cursor: pointer; border: 1px solid var(--border); border-radius: 8px;',
                onClick: function () {
                    Router.navigate('admin/overview/' + shard.dossier_id);
                }
            }, [
                Dom.el('div', { style: 'display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;' }, [
                    Dom.el('strong', {}, [shard.name || 'Sans nom']),
                    Dom.el('span', { style: 'color: var(--text-muted); font-size: var(--font-size-sm);' }, [shard.dossier_id.slice(0, 12) + '...'])
                ]),
                Dom.el('div', { style: 'display: flex; gap: 16px; font-size: var(--font-size-sm); color: var(--text-muted);' }, [
                    Dom.el('span', {}, [String(stats.sources || 0) + ' sources']),
                    Dom.el('span', {}, [String(stats.extractions || 0) + ' extractions'])
                ])
            ]);
            grid.appendChild(card);
        });
    }

    return { render: render };
})();
