/**
 * A drawing that stands in for the picture of a hotel's login page in the demo. The real router
 * sends JPEG pictures of the page as its own browser shows it.
 */
const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="390" height="591" viewBox="0 0 390 591">
<rect width="390" height="591" fill="#f7f4ee"/>
<rect width="390" height="64" fill="#1f3a5f"/>
<text x="24" y="40" font-family="Georgia, serif" font-size="20" fill="#fff">Harbor View Hotel</text>
<text x="24" y="112" font-family="Helvetica, Arial, sans-serif" font-size="24" font-weight="bold" fill="#1d1d1f">Guest Wi-Fi</text>
<text x="24" y="138" font-family="Helvetica, Arial, sans-serif" font-size="14" fill="#555">Sign in with your room details to get online.</text>
<text x="24" y="186" font-family="Helvetica, Arial, sans-serif" font-size="14" fill="#1d1d1f">Room number</text>
<rect x="24" y="196" width="342" height="44" rx="6" fill="#fff" stroke="#9a9a9a"/>
<text x="38" y="224" font-family="Helvetica, Arial, sans-serif" font-size="16" fill="#1d1d1f">412</text>
<text x="24" y="276" font-family="Helvetica, Arial, sans-serif" font-size="14" fill="#1d1d1f">Last name</text>
<rect x="24" y="286" width="342" height="44" rx="6" fill="#fff" stroke="#1f3a5f" stroke-width="2"/>
<rect x="24" y="352" width="18" height="18" rx="3" fill="#fff" stroke="#9a9a9a"/>
<text x="52" y="366" font-family="Helvetica, Arial, sans-serif" font-size="14" fill="#1d1d1f">I accept the terms of use</text>
<rect x="24" y="394" width="342" height="48" rx="6" fill="#1f3a5f"/>
<text x="195" y="424" font-family="Helvetica, Arial, sans-serif" font-size="16" font-weight="bold" fill="#fff" text-anchor="middle">Connect</text>
<text x="24" y="560" font-family="Helvetica, Arial, sans-serif" font-size="12" fill="#888">Need help? Call the front desk: dial 0.</text>
</svg>`;

export const LOGIN_PAGE_PICTURE = `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
