var SpacesView = (function () {
    function render(container) {
        Dom.clear(container);

        var header = Dom.el('div', { class: 'page-header' }, [
            Dom.el('div', {}, [
                Dom.el('h1', { class: 'page-title' }, ['Espaces de veille']),
                Dom.el('p', { class: 'page-subtitle' }, ['Chaque espace isole ses sources, extractions et recherches.'])
            ]),
            Dom.el('div', { class: 'page-actions' }, [
                Dom.el('button', { class: 'btn btn-primary', onClick: showCreateForm }, ['+ Nouvel espace'])
            ])
        ]);
        container.appendChild(header);

        var grid = Dom.el('div', { class: 'space-grid', id: 'spaces-grid' });
        container.appendChild(grid);

        loadSpaces();
    }

    function loadSpaces() {
        Api.get('/api/dossiers').then(function (spaces) {
            State.set('spaces', spaces || []);
            renderGrid(spaces || []);
        });
    }

    function renderGrid(spaces) {
        var grid = document.getElementById('spaces-grid');
        if (!grid) return;
        Dom.clear(grid);

        if (spaces.length === 0) {
            grid.appendChild(Dom.el('div', { class: 'empty-state' }, [
                Dom.el('div', { class: 'empty-state-icon' }, ['\u{1f4e1}']),
                Dom.el('div', { class: 'empty-state-text' }, ['Aucun espace. Cr\u00e9ez-en un pour commencer.']),
                Dom.el('button', { class: 'btn btn-primary', onClick: showCreateForm }, ['Cr\u00e9er un espace'])
            ]));
            return;
        }

        spaces.forEach(function (s) {
            var card = Dom.el('div', { class: 'card card-clickable', onClick: function () {
                Router.navigate(s.id);
            }}, [
                Dom.el('div', { class: 'card-title' }, [s.name || s.id]),
                Dom.el('div', { class: 'card-meta' }, [s.id.slice(0, 8) + '...']),
                Dom.el('div', { style: 'margin-top: 12px; display: flex; justify-content: flex-end;' }, [
                    Dom.el('button', { class: 'btn btn-danger btn-sm', onClick: function (e) {
                        e.stopPropagation();
                        Modal.confirm('Supprimer cet espace ?',
                            'Toutes les sources et extractions seront supprim\u00e9es.',
                            function () {
                                Api.del('/api/dossiers/' + s.id).then(function () {
                                    Toast.success('Espace supprim\u00e9');
                                    loadSpaces();
                                });
                            });
                    }}, ['Supprimer'])
                ])
            ]);
            grid.appendChild(card);
        });
    }

    function showCreateForm() {
        Modal.form('Nouvel espace', [
            { name: 'name', label: 'Nom', type: 'text', required: true, placeholder: 'Ex: Veille tech, Veille juridique...' }
        ], function (data) {
            Api.post('/api/dossiers', { name: data.name }).then(function (space) {
                Toast.success('Espace cr\u00e9\u00e9');
                Router.navigate(space.id);
            });
        });
    }

    return { render: render };
})();
