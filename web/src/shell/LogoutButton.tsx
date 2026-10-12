import { useNavigate } from 'react-router';
import { useDevice } from '@/data/device';
import { cn } from '@/lib/cn';
import { useAction } from '@/lib/useAction';
import { ActivityIndicator, Icon, IconButton, useErrorToast } from '@/ui';

/**
 * Logs out, then starts the next person on the Status page. A glass circle in the phone's bar;
 * a labelled row at the foot of the desktop sidebar.
 */
export function LogoutButton({ look }: { look: 'bar' | 'sidebar' }) {
  const { logOut } = useDevice();
  const navigate = useNavigate();
  const loggingOut = useAction(async () => {
    await logOut();
    navigate('/', { replace: true });
  }, useErrorToast());

  if (look === 'bar') {
    return (
      <IconButton
        look="glass"
        icon="logout"
        ariaLabel="Log out"
        loading={loggingOut.pending}
        onClick={() => loggingOut.run()}
      />
    );
  }

  return (
    <button
      type="button"
      aria-busy={loggingOut.pending || undefined}
      onClick={() => loggingOut.run()}
      className={cn(
        'flex min-h-8 w-full items-center gap-2.5 rounded-[8px] px-2 text-body text-label-2 transition-colors hover:bg-fill-2 hover:text-label active:bg-fill',
        loggingOut.pending && 'cursor-wait',
      )}
    >
      <span className="grid w-6 place-items-center">
        {loggingOut.pending ? <ActivityIndicator size={16} /> : <Icon name="logout" size={18} />}
      </span>
      Log out
    </button>
  );
}
