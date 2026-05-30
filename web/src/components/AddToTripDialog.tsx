import { Dialog, DialogContent, DialogTitle, Typography } from '@mui/material';

interface AddToTripDialogProps {
  open: boolean;
  /** The trip to add the plan to; may be null when opened from the trip list
   * before a trip is chosen. */
  tripId: number | null;
  onClose: () => void;
}

/** Capture dialog (spec §11): tabs Manual / Paste / Upload / Email, all hitting
 * the ingest endpoints, then a confirm step listing proposed plans. Wave 0b
 * placeholder shell; Wave 2C builds the tabbed form + confirm step on top of
 * the `ingest` / `confirmIngest` store actions. */
export default function AddToTripDialog({ open, onClose }: AddToTripDialogProps) {
  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>Add to trip</DialogTitle>
      <DialogContent>
        <Typography color="text.secondary" sx={{ py: 2 }}>
          Manual / Paste / Upload / Email capture coming soon.
        </Typography>
      </DialogContent>
    </Dialog>
  );
}
