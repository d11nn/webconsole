import React, { useContext, useEffect, useState } from "react";
import ListItemButton from "@mui/material/ListItemButton";
import ListItemIcon from "@mui/material/ListItemIcon";
import ListItemText from "@mui/material/ListItemText";
import BarChartIcon from "@mui/icons-material/BarChart";
import PhoneAndroid from "@mui/icons-material/PhoneAndroid";
import FontDownload from "@mui/icons-material/FontDownload";
import SupervisorAccountOutlinedIcon from "@mui/icons-material/SupervisorAccountOutlined";
import AttachMoneyOutlinedIcon from "@mui/icons-material/AttachMoneyOutlined";
import PersonIcon from "@mui/icons-material/Person";

import { Link } from "react-router-dom";
import { LoginContext } from "./LoginContext";
import axios from "./axios";

export const MainListItems = () => {
  const context = useContext(LoginContext);
  if (context === undefined) throw new Error("LoginContext provider required");
  const { user } = context;
  const [subscriberAdmin, setSubscriberAdmin] = useState(false);
  useEffect(() => {
    let active = true;
    axios.get("/api/rbac/me").then(response => {
      if (active) setSubscriberAdmin(response.data.permissions.includes("subscriber:manage") || response.data.permissions.includes("tenant:manage:tenant"));
    }).catch(() => {
      // Legacy subscriber navigation when the opt-in integration is disabled.
      // This grants no host deployment capability; all APIs authorize independently.
      if (active) setSubscriberAdmin(user?.username === "admin");
    });
    return () => { active = false; };
  }, [user]);
  const isAdmin = () => subscriberAdmin;

  return (
    <React.Fragment>
<Link to="/agent-team" style={{ color:"inherit",textDecoration:"inherit" }}><ListItemButton><ListItemIcon><SupervisorAccountOutlinedIcon /></ListItemIcon><ListItemText primary="AGENT TEAM" /></ListItemButton></Link>
 <Link to="/deployments" style={{ color: "inherit", textDecoration: "inherit" }}><ListItemButton><ListItemText primary="部署與 Agent 授權" /></ListItemButton></Link>
      <Link to="/status" style={{ color: "inherit", textDecoration: "inherit" }}>
        <ListItemButton>
          <ListItemIcon>
            <BarChartIcon />
          </ListItemIcon>
          <ListItemText primary="REALTIME STATUS" />
        </ListItemButton>
      </Link>
      <Link to="/subscriber" style={{ color: "inherit", textDecoration: "inherit" }}>
        <ListItemButton>
          <ListItemIcon>
            <PhoneAndroid />
          </ListItemIcon>
          <ListItemText primary="SUBSCRIBERS" />
        </ListItemButton>
      </Link>
      <Link to="/profile" style={{ color: "inherit", textDecoration: "inherit" }}>
        <ListItemButton>
          <ListItemIcon>
            <PersonIcon />
          </ListItemIcon>
          <ListItemText primary="PROFILE" />
        </ListItemButton>
      </Link>
      <Link to="/analysis" style={{ color: "inherit", textDecoration: "inherit" }}>
        <ListItemButton>
          <ListItemIcon>
            <FontDownload />
          </ListItemIcon>
          <ListItemText primary="ANALYSIS" />
        </ListItemButton>
      </Link>
      {isAdmin() ? (
        <Link to="/tenant" style={{ color: "inherit", textDecoration: "inherit" }}>
          <ListItemButton>
            <ListItemIcon>
              <SupervisorAccountOutlinedIcon />
            </ListItemIcon>
            <ListItemText primary="TENANT AND USER" />
          </ListItemButton>
        </Link>
      ) : (
        <div />
      )}
      <Link to="/charging" style={{ color: "inherit", textDecoration: "inherit" }}>
        <ListItemButton>
          <ListItemIcon>
            <AttachMoneyOutlinedIcon />
          </ListItemIcon>
          <ListItemText primary="UE CHARGING" />
        </ListItemButton>
      </Link>
    </React.Fragment>
  );
};
