var AdminUsersView = (function () {
    function render(container) {
        Dom.clear(container);
        container.appendChild(Dom.el('div', { class: 'page-header' }, [
            Dom.el('h1', { class: 'page-title' }, ['Utilisateurs']),
            Dom.el('div', { class: 'page-actions' }, [
                Dom.el('button', { class: 'btn btn-primary', onClick: showAddForm }, ['+ Cr\u00e9er'])
            ])
        ]));

        var tableWrap = Dom.el('div', { id: 'users-table' });
        container.appendChild(tableWrap);
        loadUsers();
    }

    function loadUsers() {
        Api.get('/api/admin/users').then(function (users) {
            renderTable(users || []);
        });
    }

    function renderTable(users) {
        var wrap = document.getElementById('users-table');
        if (!wrap) return;
        Dom.clear(wrap);

        if (users.length === 0) {
            wrap.appendChild(Dom.el('div', { class: 'empty-state' }, [
                Dom.el('div', { class: 'empty-state-text' }, ['Aucun utilisateur.'])
            ]));
            return;
        }

        var table = Dom.el('table', { class: 'data-table' });
        table.appendChild(Dom.el('thead', {}, [
            Dom.el('tr', {}, [
                Dom.el('th', {}, ['Nom']),
                Dom.el('th', {}, ['Email']),
                Dom.el('th', {}, ['R\u00f4le']),
                Dom.el('th', {}, ['Statut']),
                Dom.el('th', {}, ['Cr\u00e9\u00e9']),
                Dom.el('th', {}, ['Actions'])
            ])
        ]));

        var tbody = Dom.el('tbody');
        users.forEach(function (user) {
            var roleBadge = Dom.el('span', {
                class: 'badge badge-' + (user.role === 'admin' ? 'ok' : 'pending')
            }, [user.role]);

            var statusBadge2 = Dom.el('span', {
                class: 'badge badge-' + (user.status === 'active' ? 'ok' : 'error')
            }, [user.status]);

            var row = Dom.el('tr', {}, [
                Dom.el('td', {}, [user.name || '-']),
                Dom.el('td', {}, [user.email || '-']),
                Dom.el('td', {}, [roleBadge]),
                Dom.el('td', {}, [statusBadge2]),
                Dom.el('td', {}, [relTime(user.created_at)]),
                Dom.el('td', { class: 'cell-actions' }, [
                    Dom.el('button', {
                        class: 'btn btn-danger btn-sm',
                        title: 'Supprimer',
                        onClick: function () {
                            Modal.confirm('Supprimer cet utilisateur ?', user.name + ' (' + user.email + ')', function () {
                                Api.del('/api/admin/users/' + user.id).then(function () {
                                    Toast.success('Utilisateur supprim\u00e9');
                                    loadUsers();
                                });
                            });
                        }
                    }, ['\u2715'])
                ])
            ]);
            tbody.appendChild(row);
        });
        table.appendChild(tbody);
        wrap.appendChild(table);
    }

    function showAddForm() {
        Modal.form('Nouvel utilisateur', [
            { name: 'name', label: 'Nom', type: 'text', required: true },
            { name: 'email', label: 'Email', type: 'text', required: true },
            { name: 'password', label: 'Mot de passe', type: 'password', required: true },
            { name: 'role', label: 'R\u00f4le', type: 'select', options: ['user', 'admin'], value: 'user' }
        ], function (data) {
            Api.post('/api/admin/users', {
                name: data.name,
                email: data.email,
                password: data.password,
                role: data.role
            }).then(function () {
                Toast.success('Utilisateur cr\u00e9\u00e9');
                loadUsers();
            });
        });
    }

    return { render: render };
})();
