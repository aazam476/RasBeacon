import { Fragment, useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { HTTPSSettings } from '@/data/types';
import { useAction } from '@/lib/useAction';
import { Loaded } from '@/shell/Loaded';
import {
  AlertDialog,
  buttonClasses,
  ButtonRow,
  FileField,
  FormActions,
  FormSuccess,
  Notice,
  Row,
  Section,
  SwitchRow,
} from '@/ui';

/** HTTPS for these settings pages, the certificate's fingerprint, and uploading your own. */
export function HttpsScreen() {
  const { httpsSettings, reload } = useDevice();

  return (
    <Loaded data={httpsSettings} what="the HTTPS settings" onRetry={() => reload('httpsSettings')}>
      {(settings) => (
        <>
          <HttpsSwitch settings={settings} />
          <CertificateSection settings={settings} />
          <UploadCertificate />
        </>
      )}
    </Loaded>
  );
}

function HttpsSwitch({ settings }: { settings: HTTPSSettings }) {
  const { setup, status, setHttpsEnabled } = useDevice();
  const [message, setMessage] = useState<string | null>(null);
  // The address this page was opened at is the last resort: it reaches RasBeacon too.
  const address = setup?.lanAddress || status.data?.hotspot.lanAddress || window.location.hostname;
  const httpsAddress = `https://${address}/`;
  const onHttps = window.location.protocol === 'https:';

  return (
    <>
      <Section
        footer={`Encrypts the connection between your browser and these pages. When on, they move to ${httpsAddress}`}
      >
        <SwitchRow
          leading={{ tile: 'lock', color: 'blue' }}
          title="Use HTTPS for these settings pages"
          checked={settings.enabled}
          onChange={async (enabled) => {
            await setHttpsEnabled(enabled);
            setMessage(
              enabled
                ? `HTTPS is on. In a few seconds, continue at ${httpsAddress} (your browser will warn about the certificate: compare the fingerprint below).`
                : 'HTTPS is off.',
            );
          }}
        />
        {message && (
          <div className="row">
            <FormSuccess>{message}</FormSuccess>
          </div>
        )}
      </Section>
      {settings.enabled && !onHttps && (
        <Notice
          tone="info"
          icon="lock_open"
          actions={
            <a href={httpsAddress} className={buttonClasses('secondary', 'sm')}>
              Continue at {httpsAddress}
            </a>
          }
        >
          <p>You are on plain HTTP.</p>
        </Notice>
      )}
    </>
  );
}

/** The certificate in use. The fingerprint is large: people compare it with their browser's warning. */
function CertificateSection({ settings }: { settings: HTTPSSettings }) {
  const { makeNewCertificate } = useDevice();
  const [confirming, setConfirming] = useState(false);
  const [made, setMade] = useState(false);

  return (
    <Section
      title="Certificate"
      footer="RasBeacon makes its own certificate, so browsers show a warning the first time: check that the fingerprint your browser shows matches this one, then continue."
    >
      {settings.fingerprint ? (
        <>
          <div className="row flex-col items-stretch gap-1">
            <span className="text-subhead text-label-2">SHA-256 fingerprint</span>
            {/* One tap selects the whole fingerprint, ready to compare or copy. Lines break only
                after a colon, so no pair of digits is split. */}
            <p className="selectable font-mono text-body leading-7 tracking-wide text-tint select-all">
              {settings.fingerprint.split(':').map((pair, index) => (
                <Fragment key={index}>
                  {index > 0 && (
                    <>
                      :<wbr />
                    </>
                  )}
                  {pair}
                </Fragment>
              ))}
            </p>
          </div>
          <Row title="Name" value={settings.subject} />
          <Row title="Made by" value={settings.uploaded ? 'You (uploaded)' : 'RasBeacon'} />
          <Row title="Valid until" value={settings.validUntil} />
        </>
      ) : (
        <Row title={<span className="text-label-2">None yet: one is made when you turn HTTPS on.</span>} />
      )}
      {made && (
        <div className="row">
          <FormSuccess>New certificate made.</FormSuccess>
        </div>
      )}
      <ButtonRow onClick={() => setConfirming(true)}>Make a new certificate</ButtonRow>

      {confirming && (
        <AlertDialog
          title="Make a new certificate?"
          confirmLabel="Make new"
          busyLabel="Making…"
          onConfirm={async () => {
            await makeNewCertificate();
            setMade(true);
          }}
          onClose={() => setConfirming(false)}
        >
          <p>Browsers will warn about it again once, and its fingerprint changes.</p>
        </AlertDialog>
      )}
    </Section>
  );
}

/** Your own certificate instead of RasBeacon's: it needs the certificate and its private key. */
function UploadCertificate() {
  const { uploadCertificate } = useDevice();
  const [certificate, setCertificate] = useState<File | null>(null);
  const [privateKey, setPrivateKey] = useState<File | null>(null);
  // File inputs can't be emptied from code; a new key gives fresh ones after an upload.
  const [inputsKey, setInputsKey] = useState(0);
  const [uploaded, setUploaded] = useState(false);
  const uploading = useAction(async () => {
    if (certificate && privateKey) await uploadCertificate({ certificate, privateKey });
  });

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await uploading.run()) {
      setCertificate(null);
      setPrivateKey(null);
      setInputsKey((key) => key + 1);
      setUploaded(true);
    }
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4" key={inputsKey}>
      <Section title="Your own certificate" footer="Both files are needed: the certificate and its private key.">
        <FileField
          label="Certificate file"
          labelNote="(PEM)"
          accept=".pem,.crt"
          onChange={(files) => {
            setCertificate(files[0] ?? null);
            setUploaded(false);
          }}
        />
        <FileField
          label="Private key file"
          labelNote="(PEM, without a password)"
          accept=".pem,.key"
          onChange={(files) => {
            setPrivateKey(files[0] ?? null);
            setUploaded(false);
          }}
        />
      </Section>
      <FormActions
        label="Upload"
        busyLabel="Uploading…"
        pending={uploading.pending}
        disabled={!certificate || !privateKey}
        error={uploading.error}
        success={uploaded && 'Certificate uploaded.'}
      />
    </form>
  );
}
