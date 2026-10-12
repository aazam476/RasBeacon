import { Fragment } from 'react';

/**
 * Text that may wrap after "_", "-", ".", ":", "/" or "@" instead of in the middle of a word,
 * for long names without spaces such as SSIDs, host names and server addresses:
 * "GrandHyatt_Staff_" / "Secure", "vpn.example.com:" / "51820".
 */
export function BreakableText({ children }: { children: string }) {
  return children.split(/(?<=[_\-.:/@])/).map((part, index) => (
    <Fragment key={index}>
      {index > 0 && <wbr />}
      {part}
    </Fragment>
  ));
}
