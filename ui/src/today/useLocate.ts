import { useState } from "react";

import {
  coarsePoint,
  locateHere,
  PlaceNotFoundError,
  type LocatedTown,
} from "../api";

// A town is all that is needed: no GPS fix, a position up to ten minutes old
// will do.
const positionOptions: PositionOptions = {
  enableHighAccuracy: false,
  maximumAge: 10 * 60 * 1000,
  timeout: 15 * 1000,
};

const geolocationDenied = 1;

// positionWait bounds the whole ask: the browser's timeout starts only once
// the user allowed it, and a prompt dismissed without an answer (some
// browsers on "not now") never calls back.
const positionWait = 30 * 1000;

function currentPosition(): Promise<GeolocationPosition> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(
      () => reject({ code: 3 } as GeolocationPositionError),
      positionWait,
    );
    navigator.geolocation.getCurrentPosition(
      (position) => {
        window.clearTimeout(timer);
        resolve(position);
      },
      (err) => {
        window.clearTimeout(timer);
        reject(err);
      },
      positionOptions,
    );
  });
}

/** Why locating failed: access denied, no position, no town there, or the
 * map provider busy (a retry may answer). */
export type LocateFailure = "denied" | "failed" | "noTown" | "retry";

// canGeolocate reports whether this browser can be asked for a position.
export function canGeolocate(): boolean {
  return typeof navigator !== "undefined" && "geolocation" in navigator;
}

// useLocate asks the browser for its position and keeps the town there as
// where the user is (over the memory's places). locate resolves to the town,
// or null with failure set.
export function useLocate() {
  const [locating, setLocating] = useState(false);
  const [failure, setFailure] = useState<LocateFailure | null>(null);

  async function locate(): Promise<LocatedTown | null> {
    if (locating) return null;
    setLocating(true);
    setFailure(null);
    try {
      let position: GeolocationPosition;
      try {
        position = await currentPosition();
      } catch (err) {
        setFailure(
          (err as GeolocationPositionError | undefined)?.code ===
            geolocationDenied
            ? "denied"
            : "failed",
        );
        return null;
      }
      try {
        return await locateHere(
          coarsePoint(position.coords.latitude, position.coords.longitude),
        );
      } catch (err) {
        // Anything but no town is the map provider busy or away.
        setFailure(err instanceof PlaceNotFoundError ? "noTown" : "retry");
        return null;
      }
    } finally {
      setLocating(false);
    }
  }

  return { locating, failure, locate, clearFailure: () => setFailure(null) };
}
