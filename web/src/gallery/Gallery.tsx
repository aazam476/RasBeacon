// The design gallery: shows the samples from samples.tsx one at a time
// (pick one from the list) or all together, each in a phone-sized frame.
// Open it with `npm run gallery`.

import { useState } from "react";
import { SAMPLES } from "./samples";

export function Gallery() {
  const [index, setIndex] = useState(0);
  const [all, setAll] = useState(false);
  const shown = all ? SAMPLES : [SAMPLES[index]];

  return (
    <div className="gallery">
      <div className="gallery-controls">
        <strong>RasBeacon design gallery</strong>{" "}
        <select value={index} onChange={(e) => setIndex(Number(e.target.value))} disabled={all}>
          {SAMPLES.map((s, i) => (
            <option key={i} value={i}>
              {s.screen}: {s.state}
            </option>
          ))}
        </select>{" "}
        <label className="gallery-all">
          <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} /> Show
          all
        </label>
        <p>Sample data only: buttons and inputs do nothing here.</p>
      </div>
      {shown.map((s) => (
        <figure key={s.screen + s.state} className="gallery-sample">
          <figcaption>
            {s.screen}: {s.state}
          </figcaption>
          <div className="gallery-phone">{s.render()}</div>
        </figure>
      ))}
    </div>
  );
}
