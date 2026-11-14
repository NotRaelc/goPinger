const $ = id => document.getElementById(id);
const STORAGE_KEY = 'gp_user';

let authMode = 'login';
const showError = (msg = '') => { $('lErr').textContent = msg; };

function switchTab(mode) {
  authMode = mode;
  $('tabLogin').classList.toggle('on', mode === 'login');
  $('tabReg').classList.toggle('on', mode === 'register');

  const lPass2 = $('lPass2');
  if (lPass2) lPass2.hidden = mode !== 'register';

  $('authBtn').textContent = mode === 'login' ? 'ВОЙТИ' : 'СОЗДАТЬ АККАУНТ';
  $('lPass').autocomplete = mode === 'login' ? 'current-password' : 'new-password';
  showError('');
}

async function submitAuth(event) {
  event.preventDefault();
  showError('');

  const username = $('lUser').value.trim();
  const password = $('lPass').value;
  const passwordConfirm = $('lPass2')?.value ?? '';

  if (!username || !password) return showError('Заполните оба поля');
  if (authMode === 'register') {
    if (password.length < 8)          return showError('Пароль должен быть не короче 8 символов');
    if (password !== passwordConfirm) return showError('Пароли не совпадают');
  }

  const url  = authMode === 'login' ? '/login' : '/register';
  const body = authMode === 'login'
    ? { username, password }
    : { username, password, password_confirm: passwordConfirm };

  $('authBtn').disabled = true;
  try {
    const r = await fetch(url, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (r.ok) {
      localStorage.setItem(STORAGE_KEY, username);
      location.replace('/app');
      return;
    }
    if (authMode === 'login') {
      if      (r.status === 401) showError('Неверный логин или пароль');
      else if (r.status === 429) showError('Слишком много попыток. Подождите минуту.');
      else if (r.status === 400) showError('Некорректные данные');
      else                       showError('Ошибка входа (' + r.status + ')');
    } else {
      if      (r.status === 409) showError('Пользователь уже существует');
      else if (r.status === 429) showError('Слишком много попыток. Подождите минуту.');
      else if (r.status === 400) showError('Некорректные данные (пароль ≥8 символов)');
      else                       showError('Ошибка регистрации (' + r.status + ')');
    }
  } catch {
    showError('Ошибка соединения с сервером');
  } finally {
    $('authBtn').disabled = false;
  }
}

$('tabLogin').onclick  = () => switchTab('login');
$('tabReg').onclick    = () => switchTab('register');
$('authForm').onsubmit = submitAuth;