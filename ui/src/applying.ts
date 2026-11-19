// Connect takes the extension's page down while the extension restarts after a
// save, and shows its own "no extension page" message in its place. A notice
// put straight on the document is not part of what Connect swaps, so it stays
// over the whole app until the extension is back.
const ID = 'smart-plugs-applying';
// A notice never outlives this, so a restart that fails cannot leave the app
// covered.
const LONGEST_MS = 30_000;

export const hideApplying = (): void => {
  document.getElementById(ID)?.remove();
};

export const showApplying = (heading: string, detail: string): void => {
  hideApplying();

  const cover = document.createElement('div');
  cover.id = ID;
  cover.setAttribute('role', 'status');
  cover.style.cssText = [
    'position:fixed',
    'inset:0',
    'z-index:2147483000',
    'display:flex',
    'flex-direction:column',
    'align-items:center',
    'justify-content:center',
    'gap:0.5rem',
    'padding:1rem',
    'text-align:center',
    'background:var(--background, #111)',
    'color:var(--foreground, #eee)',
  ].join(';');

  const title = document.createElement('p');
  title.textContent = heading;
  title.style.cssText = 'font-size:1rem;font-weight:500;margin:0';
  const note = document.createElement('p');
  note.textContent = detail;
  note.style.cssText = 'font-size:0.875rem;opacity:0.7;margin:0';

  cover.append(title, note);
  cover.addEventListener('click', hideApplying);
  document.body.append(cover);

  setTimeout(hideApplying, LONGEST_MS);
};
