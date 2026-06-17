import { useState } from 'react';
import { Link as RouterLink, Outlet, useLocation } from 'react-router-dom';
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
import LightModeIcon from '@mui/icons-material/LightMode';
import LogoutIcon from '@mui/icons-material/Logout';
import NotificationsIcon from '@mui/icons-material/NotificationsOutlined';
import PeopleIcon from '@mui/icons-material/PeopleOutline';
import HelpOutlineIcon from '@mui/icons-material/HelpOutline';
import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined';
import SettingsBrightnessIcon from '@mui/icons-material/SettingsBrightness';
import SettingsIcon from '@mui/icons-material/SettingsOutlined';

import { useStore } from '../state/store';
import { userInitial, userName } from '../lib/format';
import { useThemeMode, type ThemePreference } from '../theme';
import AboutDialog from './AboutDialog';
import AdminDialog from './AdminDialog';
import HelpPanel from './HelpPanel';
import AlertsDialog from './AlertsDialog';
import FriendsDialog from './FriendsDialog';
import StatsDialog from './StatsDialog';
import CalendarSubscribeDialog from './CalendarSubscribeDialog';
import PreferencesDialog from './PreferencesDialog';
import AerlyLogo from './AerlyLogo';

/** The authenticated app chrome for the trip-planning redesign (spec §11).
 *
 * Holds the top bar (Trips / Tracker nav, a Help button, and the account menu)
 * plus the account-level dialogs and the help panel, and renders the routed page
 * via `<Outlet>`. Plan capture now lives on the trip page (the "New plan"
 * action), not in this global chrome. */
export default function Layout() {
  const me = useStore((s) => s.me);
  const logout = useStore((s) => s.logout);
  const logoutAll = useStore((s) => s.logoutAll);
  const openHelp = useStore((s) => s.openHelp);
  const pendingRequests = useStore((s) => s.notifications.friend_requests_pending);
  const unreadAlerts = useStore((s) => s.notifications.unread_alerts);
  const unreadShares = useStore((s) => s.notifications.unread_shares);
  const { preference: themePreference, setPreference: setThemePreference } = useThemeMode();
  const location = useLocation();
  const theme = useTheme();
  // Below `sm` (≈phones, e.g. iPhone SE) the three nav labels won't fit beside
  // the brand + account icons without wrapping, so they collapse into a drawer.
  const isNarrow = useMediaQuery(theme.breakpoints.down('sm'));

  const [adminOpen, setAdminOpen] = useState(false);
  const [aboutOpen, setAboutOpen] = useState(false);
  const [navDrawerOpen, setNavDrawerOpen] = useState(false);
  const [friendsOpen, setFriendsOpen] = useState(false);
  const [statsOpen, setStatsOpen] = useState(false);
  const [alertsOpen, setAlertsOpen] = useState(false);
  const [subscribeOpen, setSubscribeOpen] = useState(false);
  const [prefsOpen, setPrefsOpen] = useState(false);
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

  // A non-interactive caption row that heads a group of menu items.
  const sectionLabel = (text: string) => (
    <MenuItem disabled sx={{ opacity: '1 !important' }}>
      <Typography variant="caption" color="text.secondary">
        {text}
      </Typography>
    </MenuItem>
  );

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
          <AerlyLogo size={28} sx={{ mr: 1 }} />
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
            badgeContent={pendingRequests + unreadAlerts + unreadShares}
            color="error"
            overlap="circular"
            invisible={pendingRequests + unreadAlerts + unreadShares === 0}
            anchorOrigin={{ vertical: 'top', horizontal: 'right' }}
          >
            <Tooltip title="Account menu">
              <IconButton
                size="small"
                onClick={(e) => setMenuAnchor(e.currentTarget)}
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
            {sectionLabel('Activity')}
            <MenuItem
              onClick={() => {
                closeMenu();
                setAlertsOpen(true);
              }}
            >
              <ListItemIcon>
                <NotificationsIcon fontSize="small" />
              </ListItemIcon>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexGrow: 1 }}>
                <Box>Alerts…</Box>
                {unreadAlerts + unreadShares > 0 && (
                  <Chip
                    label={unreadAlerts + unreadShares}
                    size="small"
                    color="error"
                    sx={{ ml: 'auto' }}
                  />
                )}
              </Box>
            </MenuItem>
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

            <Divider />
            {sectionLabel('Your travel')}
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
                setSubscribeOpen(true);
              }}
            >
              <ListItemIcon>
                <CalendarMonthIcon fontSize="small" />
              </ListItemIcon>
              Subscribe to calendar…
            </MenuItem>

            <Divider />
            {sectionLabel('Settings')}
            <MenuItem
              onClick={() => {
                closeMenu();
                setPrefsOpen(true);
              }}
            >
              <ListItemIcon>
                <SettingsIcon fontSize="small" />
              </ListItemIcon>
              Preferences…
            </MenuItem>
            {me?.is_superuser && (
              <MenuItem
                onClick={() => {
                  closeMenu();
                  setAboutOpen(true);
                }}
              >
                <ListItemIcon>
                  <InfoOutlinedIcon fontSize="small" />
                </ListItemIcon>
                About Aerly…
              </MenuItem>
            )}

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
            <MenuItem
              onClick={() => {
                closeMenu();
                void logoutAll();
              }}
            >
              <ListItemIcon>
                <LogoutIcon fontSize="small" />
              </ListItemIcon>
              Sign out everywhere
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
      <AboutDialog open={aboutOpen} onClose={() => setAboutOpen(false)} />
      <FriendsDialog open={friendsOpen} onClose={() => setFriendsOpen(false)} />
      <StatsDialog open={statsOpen} onClose={() => setStatsOpen(false)} />
      <AlertsDialog open={alertsOpen} onClose={() => setAlertsOpen(false)} />
      <CalendarSubscribeDialog
        open={subscribeOpen}
        onClose={() => setSubscribeOpen(false)}
        scope="me"
      />
      <PreferencesDialog open={prefsOpen} onClose={() => setPrefsOpen(false)} />
      <HelpPanel />
    </Box>
  );
}
