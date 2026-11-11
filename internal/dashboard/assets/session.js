'use strict';
(() => {
  const proof = location.hash.slice(1);
  history.replaceState(null, '', '/');
  const message = document.getElementById('message');
  const button = document.getElementById('connect');
  const valid = /^[a-f0-9]{64}$/.test(proof);
  button.disabled = !valid;
  if (valid) message.textContent = 'Ready. Select Read snapshot to connect.';
  // Import presentation only after the secret has been removed from the URL.
  const frontend = import('/view.js');
  button.addEventListener('click', async () => {
    button.disabled = true;
    try {
      const {load} = await frontend;
      const response = await fetch('/api/snapshot', {
        headers: {'X-Nibfin-Session': proof},
        credentials: 'omit',
        cache: 'no-store',
        redirect: 'error'
      });
      if (!response.ok) throw new Error('Unavailable');
      load(await response.json());
      message.textContent = 'Snapshot loaded. Read-only retained observations.';
    } catch {
      message.textContent = 'Snapshot unavailable. Any display below is previous data. ' +
        'Reopen a current local session link if needed.';
    } finally {
      button.disabled = !valid;
    }
  });
})();
