let sectionSequence = 0;

export function createSettingsSection(documentRef, {className = '', title, description = '', main, aside}) {
  const section = documentRef.createElement('section');
  section.className = ['settings-section', className].filter(Boolean).join(' ');

  const label = documentRef.createElement('div');
  label.className = 'section-label';
  const heading = documentRef.createElement('h2');
  heading.id = `settings-section-${sectionSequence += 1}`;
  heading.setAttribute('id', heading.id);
  heading.textContent = title;
  label.append(heading);
  if (description) {
    const copy = documentRef.createElement('p');
    copy.textContent = description;
    label.append(copy);
  }

  const primary = documentRef.createElement('div');
  primary.className = 'section-main';
  primary.append(main);
  const content = documentRef.createElement('div');
  content.className = 'section-content';
  content.append(primary);

  if (aside) {
    const secondary = documentRef.createElement('div');
    secondary.className = 'section-aside';
    secondary.append(aside);
    content.append(secondary);
  }
  section.append(label, content);
  section.setAttribute('aria-labelledby', heading.id);
  return section;
}

export function setInlineStatus(element, message, tone = 'neutral') {
  element.textContent = message;
  element.dataset.tone = tone;
}
