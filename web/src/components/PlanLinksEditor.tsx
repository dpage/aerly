import { Box, Button, Divider, IconButton, Stack, TextField, Tooltip, Typography } from '@mui/material';
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutline';

import type { PlanLinkInput } from '../api/types';

interface Props {
  links: PlanLinkInput[];
  onChange: (links: PlanLinkInput[]) => void;
  /** Read-only (offline): the rows show but can't be changed. */
  readOnly: boolean;
}

/** Links section of the plan editor (issue #109): any number of title/URL pairs
 * for tickets, boarding passes and the like. Saved with the rest of the plan. */
export default function PlanLinksEditor({ links, onChange, readOnly }: Props) {
  const update = (i: number, field: keyof PlanLinkInput, value: string) =>
    onChange(links.map((l, j) => (j === i ? { ...l, [field]: value } : l)));

  return (
    <Box>
      <Divider sx={{ mb: 1.5 }}>
        <Typography variant="caption" color="text.secondary">
          Links
        </Typography>
      </Divider>
      <Stack spacing={1.5}>
        {links.length === 0 && (
          <Typography variant="body2" color="text.secondary">
            Link to tickets, boarding passes or anything else for this plan.
          </Typography>
        )}
        {links.map((l, i) => (
          <Stack key={i} direction="row" spacing={1} alignItems="flex-start">
            <Stack spacing={1} sx={{ flexGrow: 1 }}>
              <TextField
                label="Title"
                size="small"
                value={l.title}
                onChange={(e) => update(i, 'title', e.target.value)}
                placeholder="e.g. E-ticket"
                disabled={readOnly}
                fullWidth
              />
              <TextField
                label="URL"
                size="small"
                type="url"
                value={l.url}
                onChange={(e) => update(i, 'url', e.target.value)}
                placeholder="https://…"
                disabled={readOnly}
                fullWidth
              />
            </Stack>
            {!readOnly && (
              <Tooltip title="Remove link">
                <IconButton
                  aria-label={`Remove link ${i + 1}`}
                  onClick={() => onChange(links.filter((_, j) => j !== i))}
                >
                  <DeleteOutlineIcon />
                </IconButton>
              </Tooltip>
            )}
          </Stack>
        ))}
        {!readOnly && (
          <Box>
            <Button
              variant="outlined"
              size="small"
              onClick={() => onChange([...links, { title: '', url: '' }])}
            >
              Add link
            </Button>
          </Box>
        )}
      </Stack>
    </Box>
  );
}
