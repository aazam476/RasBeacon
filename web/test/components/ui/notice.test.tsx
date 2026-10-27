// The tinted group of messages (Notice.tsx).
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NoticeList } from '@/ui/Notice';

describe('notice list', () => {
  // Two devices with the same problem give the same message twice: both show.
  it('shows a message that comes twice, twice', () => {
    render(<NoticeList ariaLabel="Warnings" messages={['Over its data limit.', 'Over its data limit.', 'Paused.']} />);
    expect(screen.getAllByText('Over its data limit.')).toHaveLength(2);
    expect(screen.getAllByRole('alert')).toHaveLength(3);
  });
});
