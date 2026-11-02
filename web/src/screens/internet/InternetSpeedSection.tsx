import { useDevice } from '@/data/device';
import type { QoSSettings, SpeedtestResult } from '@/data/types';
import { kbpsFromText, speedProblem, textFromKbps } from '@/lib/qos';
import { usePagePart } from '@/lib/pageForm';
import { useForm } from '@/lib/useForm';
import { ButtonRow, Row, Section, TextField } from '@/ui';
import { PART_ORDER } from './partOrder';

/**
 * The internet connection's speeds, which priority devices need: RasBeacon sends a little below
 * them, so the queue forms where it can put priority devices first. Hidden until there is a
 * priority device or a speed, so the page doesn't ask for something nobody uses.
 */
export function InternetSpeedSection({ lastTest }: { lastTest: SpeedtestResult | null | undefined }) {
  const { qosSettings } = useDevice();
  const qos = qosSettings.data;
  if (!qos || (qos.priorityDevices === 0 && qos.uploadKbps === 0 && qos.downloadKbps === 0)) return null;
  return <SpeedForm qos={qos} lastTest={lastTest} />;
}

/** What priority does now, in a few words. */
function state(qos: QoSSettings): string {
  if (qos.priorityDevices === 0) return 'No priority devices';
  if (qos.active) return qos.priorityDevices === 1 ? '1 device goes first' : `${qos.priorityDevices} devices go first`;
  return 'Not working';
}

function SpeedForm({ qos, lastTest }: { qos: QoSSettings; lastTest: SpeedtestResult | null | undefined }) {
  const { saveQoSSettings } = useDevice();
  const form = useForm({ download: textFromKbps(qos.downloadKbps), upload: textFromKbps(qos.uploadKbps) }, (values) =>
    saveQoSSettings({ downloadKbps: kbpsFromText(values.download), uploadKbps: kbpsFromText(values.upload) }),
  );
  const { values, setField, fieldErrors } = form;
  // Checked as typed, with the router's words.
  const downloadProblem = speedProblem(kbpsFromText(values.download));
  const uploadProblem = speedProblem(kbpsFromText(values.upload));
  usePagePart('speeds', {
    order: PART_ORDER.speeds,
    dirty: form.dirty,
    pending: form.pending,
    error: form.error,
    invalid: Boolean(downloadProblem || uploadProblem),
    submit: form.submit,
    discard: form.discard,
    title: 'Internet speed',
  });

  function fillFromTest(test: SpeedtestResult) {
    // Mb/s to one decimal, as typed.
    setField('download', textFromKbps(Math.round(test.downloadMbps * 1000)));
    setField('upload', textFromKbps(Math.round(test.uploadMbps * 1000)));
  }

  return (
    <Section
      title="Internet speed"
      footer="Priority devices on the Clients page go first when the connection is busy. RasBeacon sends a little below these speeds, so it decides what goes first instead of the hotel’s router. Leave one empty to leave that direction as it is. A new network needs its own speeds."
    >
      <div className="flex flex-col">
        <Row title="Priority devices" value={state(qos)} />
        <TextField
          label="Download"
          labelNote="(Mbps)"
          inputMode="decimal"
          placeholder="Not set"
          value={values.download}
          onChange={(download) => setField('download', download)}
          error={(values.download.trim() ? downloadProblem : undefined) ?? fieldErrors.downloadKbps}
          autoComplete="off"
        />
        <TextField
          label="Upload"
          labelNote="(Mbps)"
          inputMode="decimal"
          placeholder="Not set"
          value={values.upload}
          onChange={(upload) => setField('upload', upload)}
          error={(values.upload.trim() ? uploadProblem : undefined) ?? fieldErrors.uploadKbps}
          autoComplete="off"
        />
        {lastTest && lastTest.downloadMbps > 0 && (
          <ButtonRow onClick={() => fillFromTest(lastTest)}>
            Use the last speed test ({textFromKbps(Math.round(lastTest.downloadMbps * 1000))} down,{' '}
            {textFromKbps(Math.round(lastTest.uploadMbps * 1000))} up)
          </ButtonRow>
        )}
      </div>
    </Section>
  );
}
