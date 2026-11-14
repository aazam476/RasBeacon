/** Saves text as a file in the viewer's Downloads, as a link with `download` would. */
export function saveTextFile(fileName: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
  const link = document.createElement('a');
  link.href = url;
  link.download = fileName;
  link.click();
  // The browser has the file once the click is handled.
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
