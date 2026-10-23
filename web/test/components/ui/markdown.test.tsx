// A plugin's README, drawn with the app's own elements: never as HTML.
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Markdown } from '@/ui';

const README = `# AdGuard Home

### Setting it up

1. Open **its page**.
2. Choose *Use these servers*.

- Blocks ads
- Uses \`53/udp\`

See [the docs](https://adguard.com/docs).

\`\`\`
dns: 10.87.0.1
\`\`\`

<script>alert(1)</script>`;

describe('README', () => {
  it('draws headings one level under the section, lists, code and emphasis', () => {
    const { container } = render(<Markdown source={README} />);
    expect(screen.getByRole('heading', { level: 3, name: 'AdGuard Home' })).toBeTruthy();
    expect(screen.getByRole('heading', { level: 4, name: 'Setting it up' })).toBeTruthy();
    expect(container.querySelectorAll('ol > li')).toHaveLength(2);
    expect(container.querySelectorAll('ul > li')).toHaveLength(2);
    expect(container.querySelector('strong')?.textContent).toBe('its page');
    expect(container.querySelector('em')?.textContent).toBe('Use these servers');
    expect(container.querySelector('li code')?.textContent).toBe('53/udp');
    expect(container.querySelector('pre')?.textContent).toBe('dns: 10.87.0.1');
  });

  it('opens links in a new tab without telling the site where from', () => {
    render(<Markdown source={README} />);
    const link = screen.getByRole('link', { name: 'the docs' });
    expect(link.getAttribute('href')).toBe('https://adguard.com/docs');
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toBe('noreferrer');
  });

  it('shows HTML as text, never as elements', () => {
    const { container } = render(<Markdown source={README} />);
    expect(container.querySelector('script')).toBeNull();
    expect(screen.getByText('<script>alert(1)</script>')).toBeTruthy();
  });
});
