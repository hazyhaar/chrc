var SearchView = (function () {
    var currentSpaceId = null;

    function render(container, spaceId) {
        currentSpaceId = spaceId;
        Dom.clear(container);

        container.appendChild(Dom.el('div', { class: 'page-header' }, [
            Dom.el('h1', { class: 'page-title' }, ['Recherche'])
        ]));

        var input = Dom.el('input', {
            class: 'form-input',
            type: 'text',
            id: 'search-input',
            placeholder: 'Rechercher dans les extractions... (syntaxe FTS5 : "phrase exacte", OR, AND, terme*)'
        });

        var searchBar = Dom.el('div', { class: 'search-bar' }, [
            input,
            Dom.el('button', { class: 'btn btn-primary', onClick: doSearch }, ['Rechercher'])
        ]);
        container.appendChild(searchBar);

        Dom.on(input, 'keydown', function (e) {
            if (e.key === 'Enter') doSearch();
        });

        var results = Dom.el('div', { id: 'search-results' });
        container.appendChild(results);

        // Focus input.
        setTimeout(function () { input.focus(); }, 50);
    }

    function doSearch() {
        var input = document.getElementById('search-input');
        var q = input ? input.value.trim() : '';
        if (!q) return;

        var results = document.getElementById('search-results');
        if (!results) return;
        Dom.clear(results);
        results.appendChild(Dom.el('div', { class: 'loading' }, [Dom.el('span', { class: 'spinner' }), 'Recherche...']));

        Api.get('/api/dossiers/' + currentSpaceId + '/search?q=' + encodeURIComponent(q) + '&limit=30').then(function (data) {
            Dom.clear(results);
            if (!data || data.length === 0) {
                results.appendChild(Dom.el('div', { class: 'empty-state' }, [
                    Dom.el('div', { class: 'empty-state-text' }, ['Aucun r\u00e9sultat pour "' + q + '"'])
                ]));
                return;
            }

            results.appendChild(Dom.el('p', { style: 'color: var(--text-muted); margin-bottom: 16px; font-size: var(--font-size-sm);' },
                [data.length + ' r\u00e9sultat(s)']));

            data.forEach(function (r) {
                var preview = Dom.el('div', { class: 'extraction-preview', style: 'display: none;' });
                preview.textContent = r.text || '';

                var snippet = (r.text || '').slice(0, 200);
                if ((r.text || '').length > 200) snippet += '...';

                var card = Dom.el('div', { class: 'card', style: 'margin-bottom: 8px; cursor: pointer;', onClick: function () {
                    preview.style.display = preview.style.display === 'none' ? 'block' : 'none';
                }}, [
                    Dom.el('div', { style: 'display: flex; justify-content: space-between; align-items: center; margin-bottom: 4px;' }, [
                        Dom.el('span', { class: 'badge badge-unchanged' }, ['rank ' + (r.rank || 0).toFixed(1)]),
                        Dom.el('span', { class: 'cell-mono', style: 'color: var(--text-muted);' }, [(r.source_name || r.extraction_id || '').slice(0, 20)])
                    ]),
                    Dom.el('p', { style: 'font-size: var(--font-size-sm); color: var(--text-secondary); line-height: 1.5;' }, [snippet]),
                    preview
                ]);
                results.appendChild(card);
            });
        });
    }

    return { render: render };
})();
