'use strict';
(() => {
  const proof = location.hash.slice(1);
  history.replaceState(null, '', '/');
  const output = document.getElementById('snapshot');
  const button = document.getElementById('connect');
  button.disabled = !/^[a-f0-9]{64}$/.test(proof);
  if (!button.disabled) output.textContent = 'Ready. Select Read snapshot to connect.';
  button.addEventListener('click', async () => {
    button.disabled = true;
    try {
      const response = await fetch('/api/snapshot', {
        headers: {'X-Nibfin-Session': proof},
        credentials: 'omit',
        cache: 'no-store',
        redirect: 'error'
      });
      if (!response.ok) throw new Error('Snapshot request unavailable.');
      const snapshot = await response.json();
      output.textContent = JSON.stringify(snapshot, null, 2);
    } catch {
      output.textContent = 'Snapshot unavailable. Reopen a current local session link.';
    } finally {
      button.disabled = false;
    }
  });
})();
