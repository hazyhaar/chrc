var LoginView = (function () {
    function render(container) {
        Dom.clear(container);

        var card = Dom.el('div', { class: 'card', style: 'max-width: 360px; margin: 80px auto; padding: 32px;' });

        card.appendChild(Dom.el('h1', { style: 'font-size: 24px; margin-bottom: 24px; text-align: center;' }, ['veille']));

        var emailInput = Dom.el('input', {
            class: 'form-input',
            type: 'text',
            id: 'login-email',
            placeholder: 'Identifiant',
            autocomplete: 'username'
        });

        var passInput = Dom.el('input', {
            class: 'form-input',
            type: 'password',
            id: 'login-password',
            placeholder: 'Mot de passe',
            autocomplete: 'current-password'
        });

        var errorEl = Dom.el('div', {
            id: 'login-error',
            style: 'color: var(--danger); font-size: var(--font-size-sm); min-height: 20px; margin-bottom: 8px;'
        });

        var btn = Dom.el('button', {
            class: 'btn btn-primary',
            style: 'width: 100%;',
            onClick: doLogin
        }, ['Connexion']);

        card.appendChild(Dom.el('div', { class: 'form-group' }, [
            Dom.el('label', { class: 'form-label' }, ['Identifiant']),
            emailInput
        ]));
        card.appendChild(Dom.el('div', { class: 'form-group' }, [
            Dom.el('label', { class: 'form-label' }, ['Mot de passe']),
            passInput
        ]));
        card.appendChild(errorEl);
        card.appendChild(btn);

        container.appendChild(card);

        Dom.on(passInput, 'keydown', function (e) {
            if (e.key === 'Enter') doLogin();
        });
        Dom.on(emailInput, 'keydown', function (e) {
            if (e.key === 'Enter') passInput.focus();
        });

        setTimeout(function () { emailInput.focus(); }, 50);
    }

    function doLogin() {
        var email = document.getElementById('login-email').value.trim();
        var password = document.getElementById('login-password').value;
        var errorEl = document.getElementById('login-error');
        if (!email || !password) {
            errorEl.textContent = 'Identifiant et mot de passe requis.';
            return;
        }
        errorEl.textContent = '';

        fetch('/api/auth/login', {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ email: email, password: password })
        }).then(function (resp) {
            if (!resp.ok) {
                errorEl.textContent = 'Identifiants invalides.';
                return;
            }
            return resp.json().then(function (user) {
                State.set('user', user);
                // Reload the app now that we have a session cookie.
                initApp();
            });
        }).catch(function () {
            errorEl.textContent = 'Erreur de connexion.';
        });
    }

    return { render: render };
})();
