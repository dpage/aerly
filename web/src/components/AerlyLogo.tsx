import { Box, type SxProps, type Theme } from '@mui/material';

import { useThemeMode } from '../theme';

/** The Aerly brand mark — the metallic icon in light mode, the glowing icon in
 * dark mode. Each variant is a transparent PNG that only ever renders against
 * the matching background, so there's no edge fringing. The assets live in
 * web/public and are served from the SPA root. */
export default function AerlyLogo({
  size = 24,
  sx,
}: {
  /** Rendered width/height in pixels (the mark is square). */
  size?: number;
  sx?: SxProps<Theme>;
}) {
  const { mode } = useThemeMode();
  const src = mode === 'dark' ? '/aerly-mark-dark.png' : '/aerly-mark-light.png';
  return (
    <Box
      component="img"
      src={src}
      alt="Aerly"
      width={size}
      height={size}
      sx={{ display: 'block', flexShrink: 0, ...sx }}
    />
  );
}
