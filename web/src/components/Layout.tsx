import { useState } from 'react';
import { Link as RouterLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import {
  AppBar,
  Avatar,
  Badge,
  Box,
  Button,
  Chip,
  Divider,
  Drawer,
  IconButton,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  Toolbar,
  Tooltip,
  Typography,
  useMediaQuery,
  useTheme,
} from '@mui/material';
import AdminPanelSettingsIcon from '@mui/icons-material/AdminPanelSettings';
import MenuIcon from '@mui/icons-material/Menu';
import LuggageIcon from '@mui/icons-material/LuggageOutlined';
import RadarIcon from '@mui/icons-material/Radar';
import BarChartIcon from '@mui/icons-material/BarChart';
import CalendarMonthIcon from '@mui/icons-material/CalendarMonth';
import DarkModeIcon from '@mui/icons-material/DarkMode';
import EmailIcon from '@mui/icons-material/EmailOutlined';
import FlightTakeoffIcon from '@mui/icons-material/FlightTakeoff';
import LightModeIcon from '@mui/icons-material/LightMode';
import LogoutIcon from '@mui/icons-material/Logout';
import NotificationsIcon from '@mui/icons-material/NotificationsOutlined';
import HomeIcon from '@mui/icons-material/HomeOutlined';
import PeopleIcon from '@mui/icons-material/PeopleOutline';
import HelpOutlineIcon from '@mui/icons-material/HelpOutline';
import SettingsBrightnessIcon from '@mui/icons-material/SettingsBrightness';

import { useStore } from '../state/store';
import { userInitial, userName } from '../lib/format';
import { useThemeMode, type ThemePreference } from '../theme';
import AdminDialog from './AdminDialog';
import HelpPanel from './HelpPanel';
import AlertPrefsDialog from './AlertPrefsDialog';
import EmailsDialog from './EmailsDialog';
import FriendsDialog from './FriendsDialog';
import StatsDialog from './StatsDialog';
import CalendarSubscribeDialog from './CalendarSubscribeDialog';
import HomeAddressDialog from './HomeAddressDialog';

/** The authenticated app chrome for the trip-planning redesign (spec §11).
 *
 * Holds the top bar (Trips / Tracker nav, a Help button, and the account menu)
 * plus the account-level dialogs and the help panel, and renders the routed page
 * via `<Outlet>`. Plan capture now lives on the trip page (the "New plan"
 * action), not in this global chrome. */
export default function Layout() {
  const me = useStore((s) => s.me);
  const logout = useStore((s) => s.logout);
  const capabilities = useStore((s) => s.capabilities);
  const openHelp = useStore((s) => s.openHelp);
  const pendingRequests = useStore((s) => s.notifications.friend_requests_pending);
  const unreadAlerts = useStore((s) => s.notifications.unread_alerts);
  const alerts = useStore((s) => s.alerts);
  const markAlertsRead = useStore((s) => s.markAlertsRead);
  const { preference: themePreference, setPreference: setThemePreference } = useThemeMode();
  const location = useLocation();
  const navigate = useNavigate();
  const theme = useTheme();
  // Below `sm` (≈phones, e.g. iPhone SE) the three nav labels won't fit beside
  // the brand + account icons without wrapping, so they collapse into a drawer.
  const isNarrow = useMediaQuery(theme.breakpoints.down('sm'));

  const [adminOpen, setAdminOpen] = useState(false);
  const [navDrawerOpen, setNavDrawerOpen] = useState(false);
  const [emailsOpen, setEmailsOpen] = useState(false);
  const [friendsOpen, setFriendsOpen] = useState(false);
  const [statsOpen, setStatsOpen] = useState(false);
  const [alertPrefsOpen, setAlertPrefsOpen] = useState(false);
  const [subscribeOpen, setSubscribeOpen] = useState(false);
  const [homeAddrOpen, setHomeAddrOpen] = useState(false);
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);

  const closeMenu = () => setMenuAnchor(null);
  const onTracker = location.pathname.startsWith('/tracker');
  const onFriends = location.pathname.startsWith('/friends');
  // "My trips" owns the home view and the trip-detail pages; "Friends' trips"
  // and "Tracker" own their own routes.
  const onMyTrips = !onTracker && !onFriends;
  // Single source of truth for the primary destinations, shared by the wide
  // inline buttons and the narrow drawer so they never drift apart.
  const navItems = [
    { to: '/', label: 'My trips', active: onMyTrips, Icon: LuggageIcon },
    { to: '/friends', label: "Friends' trips", active: onFriends, Icon: PeopleIcon },
    { to: '/tracker', label: 'Tracker', active: onTracker, Icon: RadarIcon },
  ];
  // Open help to the topic relevant to the current screen: the Tracker and a
  // trip's Map tab → Map & tracker; another trip view → Plans; else → Trips.
  const helpContext =
    onTracker || location.pathname.endsWith('/map')
      ? 'tracker'
      : location.pathname.startsWith('/trips/')
        ? 'plans'
        : 'trips';

  return (
    <Box sx={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
      <AppBar position="static" color="default" elevation={1}>
        <Toolbar variant="dense">
          {isNarrow && (
            <IconButton
              size="small"
              edge="start"
              aria-label="Open navigation menu"
              onClick={() => setNavDrawerOpen(true)}
              sx={{ mr: 0.5 }}
            >
              <MenuIcon />
            </IconButton>
          )}
          <FlightTakeoffIcon color="primary" sx={{ mr: 1 }} />
          <Typography
            variant="h6"
            component={RouterLink}
            to="/"
            sx={{ flexGrow: 0, mr: isNarrow ? 0 : 3, color: 'inherit', textDecoration: 'none' }}
          >
            Aerly
          </Typography>
          {!isNarrow &&
            navItems.map((item) => (
              <Button
                key={item.to}
                component={RouterLink}
                to={item.to}
                size="small"
                color={item.active ? 'primary' : 'inherit'}
              >
                {item.label}
              </Button>
            ))}
          <Box sx={{ flexGrow: 1 }} />
          <Tooltip title="Help">
            <IconButton
              size="small"
              onClick={() => openHelp(helpContext)}
              aria-label="Help"
              sx={{ mr: 1 }}
            >
              <HelpOutlineIcon />
            </IconButton>
          </Tooltip>
          {me?.is_superuser && (
            <Tooltip title="Manage users">
              <IconButton size="small" onClick={() => setAdminOpen(true)} sx={{ mr: 1 }}>
                <AdminPanelSettingsIcon />
              </IconButton>
            </Tooltip>
          )}
          <Badge
            badgeContent={pendingRequests + unreadAlerts}
            color="error"
            overlap="circular"
            invisible={pendingRequests + unreadAlerts === 0}
            anchorOrigin={{ vertical: 'top', horizontal: 'right' }}
          >
            <Tooltip title="Account menu">
              <IconButton
                size="small"
                onClick={(e) => {
                  setMenuAnchor(e.currentTarget);
                  if (unreadAlerts > 0) void markAlertsRead();
                }}
                aria-label="Account menu"
              >
                <Avatar src={me?.avatar_url} sx={{ width: 28, height: 28 }}>
                  {me && userInitial(me)}
                </Avatar>
              </IconButton>
            </Tooltip>
          </Badge>
          <Menu
            anchorEl={menuAnchor}
            open={menuAnchor !== null}
            onClose={closeMenu}
            anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
            transformOrigin={{ vertical: 'top', horizontal: 'right' }}
          >
            {me && (
              <MenuItem disabled sx={{ opacity: '1 !important' }}>
                <Typography variant="caption" color="text.secondary">
                  Signed in as {userName(me)}
                </Typography>
              </MenuItem>
            )}
            <Divider />
            {alerts.length > 0 && (
              <Box>
                <MenuItem disabled sx={{ opacity: '1 !important' }}>
                  <Typography variant="caption" color="text.secondary">
                    Alerts
                  </Typography>
                </MenuItem>
                {alerts.slice(0, 6).map((al) => (
                  <MenuItem
                    key={al.id}
                    onClick={() => {
                      closeMenu();
                      // Reminders span all plan types, so they open the trip
                      // timeline; flight-change alerts open the flight tracker.
                      if (al.kind === 'reminder') navigate(`/trips/${al.trip_id}`);
                      else navigate(`/tracker?part=${al.plan_part_id}`);
                    }}
                  >
                    <ListItemIcon>
                      <NotificationsIcon fontSize="small" />
                    </ListItemIcon>
                    <Typography variant="body2" noWrap sx={{ maxWidth: 260 }}>
                      {al.message}
                    </Typography>
                  </MenuItem>
                ))}
                <Divider />
              </Box>
            )}
            <MenuItem
              onClick={() => {
                closeMenu();
                setFriendsOpen(true);
              }}
            >
              <ListItemIcon>
                <PeopleIcon fontSize="small" />
              </ListItemIcon>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexGrow: 1 }}>
                <Box>Friends…</Box>
                {pendingRequests > 0 && (
                  <Chip label={pendingRequests} size="small" color="error" sx={{ ml: 'auto' }} />
                )}
              </Box>
            </MenuItem>
            {capabilities.email_ingest_enabled && (
              <MenuItem
                onClick={() => {
                  closeMenu();
                  setEmailsOpen(true);
                }}
              >
                <ListItemIcon>
                  <EmailIcon fontSize="small" />
                </ListItemIcon>
                Email addresses…
              </MenuItem>
            )}
            <MenuItem
              onClick={() => {
                closeMenu();
                setStatsOpen(true);
              }}
            >
              <ListItemIcon>
                <BarChartIcon fontSize="small" />
              </ListItemIcon>
              Statistics…
            </MenuItem>
            <MenuItem
              onClick={() => {
                closeMenu();
                setAlertPrefsOpen(true);
              }}
            >
              <ListItemIcon>
                <NotificationsIcon fontSize="small" />
              </ListItemIcon>
              Alert preferences…
            </MenuItem>
            <MenuItem
              onClick={() => {
                closeMenu();
                setHomeAddrOpen(true);
              }}
            >
              <ListItemIcon>
                <HomeIcon fontSize="small" />
              </ListItemIcon>
              Home address…
            </MenuItem>
            <MenuItem
              onClick={() => {
                closeMenu();
                setSubscribeOpen(true);
              }}
            >
              <ListItemIcon>
                <CalendarMonthIcon fontSize="small" />
              </ListItemIcon>
              Subscribe to calendar…
            </MenuItem>
            <Divider />
            <MenuItem disabled sx={{ opacity: '1 !important' }}>
              <Typography variant="caption" color="text.secondary">
                Appearance
              </Typography>
            </MenuItem>
            {(
              [
                { value: 'light', label: 'Light', Icon: LightModeIcon },
                { value: 'dark', label: 'Dark', Icon: DarkModeIcon },
                { value: 'system', label: 'System', Icon: SettingsBrightnessIcon },
              ] as const
            ).map(({ value, label, Icon }) => (
              <MenuItem
                key={value}
                selected={themePreference === value}
                onClick={() => {
                  setThemePreference(value as ThemePreference);
                  closeMenu();
                }}
              >
                <ListItemIcon>
                  <Icon fontSize="small" />
                </ListItemIcon>
                {label}
              </MenuItem>
            ))}
            <Divider />
            <MenuItem
              onClick={() => {
                closeMenu();
                void logout();
              }}
            >
              <ListItemIcon>
                <LogoutIcon fontSize="small" />
              </ListItemIcon>
              Sign out
            </MenuItem>
          </Menu>
        </Toolbar>
      </AppBar>

      {isNarrow && (
        <Drawer anchor="left" open={navDrawerOpen} onClose={() => setNavDrawerOpen(false)}>
          <Box sx={{ width: 260 }} role="presentation">
            <List>
              {navItems.map(({ to, label, active, Icon }) => (
                <ListItemButton
                  key={to}
                  component={RouterLink}
                  to={to}
                  selected={active}
                  onClick={() => setNavDrawerOpen(false)}
                >
                  <ListItemIcon>
                    <Icon color={active ? 'primary' : undefined} />
                  </ListItemIcon>
                  <ListItemText primary={label} />
                </ListItemButton>
              ))}
            </List>
          </Box>
        </Drawer>
      )}

      <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto' }}>
        <Outlet />
      </Box>

      <AdminDialog open={adminOpen} onClose={() => setAdminOpen(false)} />
      <EmailsDialog open={emailsOpen} onClose={() => setEmailsOpen(false)} />
      <FriendsDialog open={friendsOpen} onClose={() => setFriendsOpen(false)} />
      <StatsDialog open={statsOpen} onClose={() => setStatsOpen(false)} />
      <AlertPrefsDialog open={alertPrefsOpen} onClose={() => setAlertPrefsOpen(false)} />
      <HomeAddressDialog open={homeAddrOpen} onClose={() => setHomeAddrOpen(false)} />
      <CalendarSubscribeDialog
        open={subscribeOpen}
        onClose={() => setSubscribeOpen(false)}
        scope="me"
      />
      <HelpPanel />
    </Box>
  );
}
