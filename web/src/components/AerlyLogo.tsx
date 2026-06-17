import { Box, type SxProps, type Theme } from '@mui/material';

/** The Aerly brand mark — a single transparent PNG designed to read well
 * against both light and dark backgrounds, so there's no need for theme-aware
 * variants. The asset lives in web/public and is served from the SPA root. */
export default function AerlyLogo({
  size = 24,
  sx,
}: {
  /** Rendered width/height in pixels (the mark is square). */
  size?: number;
  sx?: SxProps<Theme>;
}) {
  return (
    <Box
      component="img"
      src="/aerly-mark.png"
      alt="Aerly"
      width={size}
      height={size}
      sx={{ display: 'block', flexShrink: 0, ...sx }}
    />
  );
}
