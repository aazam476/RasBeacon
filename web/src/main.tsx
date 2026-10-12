import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

// The app uses the device's own system font (SF Pro on Apple devices), so nothing has to load
// before text appears, even when the phone has no internet yet (e.g. behind a captive portal).
import './index.css';

import { App } from './App';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
