"use strict";

import React, { forwardRef, memo, useCallback, useEffect, useImperativeHandle, useMemo, useRef } from 'react';
import { NativeModules } from 'react-native';
import { makeLatLngBounds, makePoint } from '../utils/geoUtils';
import NativeCameraView from '../specs/RNMBXCameraNativeComponent';
import RNMBXCameraModule from '../specs/NativeRNMBXCameraModule';
import { NativeCommands } from '../utils/NativeCommands';
import { jsx as _jsx } from "react/jsx-runtime";
const NativeModule = NativeModules.RNMBXModule;
export let UserTrackingMode = /*#__PURE__*/function (UserTrackingMode) {
  UserTrackingMode["Follow"] = "normal";
  UserTrackingMode["FollowWithHeading"] = "compass";
  UserTrackingMode["FollowWithCourse"] = "course";
  return UserTrackingMode;
}({});
/**
 * Converts the provided React Native animation mode into the corresponding native enum value.
 */
const nativeAnimationMode = mode => {
  const NativeCameraModes = NativeModule.CameraModes;
  switch (mode) {
    case 'flyTo':
      return NativeCameraModes.Flight;
    case 'easeTo':
      return NativeCameraModes.Ease;
    case 'linearTo':
      return NativeCameraModes.Linear;
    case 'moveTo':
      return NativeCameraModes.Move;
    case 'none':
      return NativeCameraModes.None;
    default:
      return NativeCameraModes.Ease;
  }
};

// Native module types.

/**
 * Controls the perspective from which the user sees the map.
 *
 * To use imperative methods, pass in a ref object:
 *
 * ```tsx
 * const camera = useRef<Camera>(null);
 *
 * useEffect(() => {
 *   camera.current?.setCamera({
 *     centerCoordinate: [lon, lat],
 *   });
 * }, []);
 *
 * return (
 *   <Camera ref={camera} />
 * );
 * ```
 */
export const Camera = /*#__PURE__*/memo(/*#__PURE__*/forwardRef((props, ref) => {
  const {
    centerCoordinate,
    bounds,
    heading,
    pitch,
    zoomLevel,
    padding,
    animationDuration,
    animationMode,
    minZoomLevel,
    maxZoomLevel,
    maxBounds,
    followUserLocation,
    followUserMode,
    followZoomLevel,
    followPitch,
    followHeading,
    followPadding,
    defaultSettings,
    allowUpdates = true,
    onUserTrackingModeChange
  } = props;
  const nativeCamera = useRef(null);
  const commands = useMemo(() => new NativeCommands(RNMBXCameraModule), []);
  useEffect(() => {
    if (nativeCamera.current) {
      commands.setNativeRef(nativeCamera.current);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [commands, nativeCamera.current]);
  const buildNativeStop = useCallback((stop, ignoreFollowUserLocation = false) => {
    stop = {
      ...stop,
      type: 'CameraStop'
    };
    if (props.followUserLocation && !ignoreFollowUserLocation) {
      return null;
    }
    const _nativeStop = {};
    if (stop.pitch !== undefined) _nativeStop.pitch = stop.pitch;
    if (stop.heading !== undefined) _nativeStop.heading = stop.heading;
    if (stop.zoomLevel !== undefined) _nativeStop.zoom = stop.zoomLevel;
    if (stop.animationMode !== undefined) _nativeStop.mode = nativeAnimationMode(stop.animationMode);
    if (stop.animationDuration !== undefined) _nativeStop.duration = stop.animationDuration;
    if (stop.centerCoordinate) {
      _nativeStop.centerCoordinate = JSON.stringify(makePoint(stop.centerCoordinate));
    }
    if (stop.bounds && stop.bounds.ne && stop.bounds.sw) {
      const {
        ne,
        sw
      } = stop.bounds;
      _nativeStop.bounds = JSON.stringify(makeLatLngBounds(ne, sw));
    }
    const paddingTop = stop.padding?.paddingTop ?? stop.bounds?.paddingTop;
    if (paddingTop !== undefined) {
      _nativeStop.paddingTop = paddingTop;
    }
    const paddingRight = stop.padding?.paddingRight ?? stop.bounds?.paddingRight;
    if (paddingRight !== undefined) {
      _nativeStop.paddingRight = paddingRight;
    }
    const paddingBottom = stop.padding?.paddingBottom ?? stop.bounds?.paddingBottom;
    if (paddingBottom != undefined) {
      _nativeStop.paddingBottom = paddingBottom;
    }
    const paddingLeft = stop.padding?.paddingLeft ?? stop.bounds?.paddingLeft;
    if (paddingLeft !== undefined) {
      _nativeStop.paddingLeft = paddingLeft;
    }
    return _nativeStop;
  }, [props.followUserLocation]);

  // since codegen uses `payload` name in cpp code for creating payload for event,
  // we rename it to `payloadRenamed` to avoid name collision there on new arch
  const _onUserTrackingModeChange = useCallback(event => {
    if (onUserTrackingModeChange) {
      if (!event.nativeEvent.payload) {
        // @ts-expect-error see the comment above
        event.nativeEvent.payload = event.nativeEvent.payloadRenamed;
      }
      onUserTrackingModeChange(event);
    }
  }, [onUserTrackingModeChange]);
  const nativeDefaultStop = useMemo(() => {
    if (!defaultSettings) {
      return null;
    }
    return buildNativeStop(defaultSettings);
  }, [defaultSettings, buildNativeStop]);
  const nativeStop = useMemo(() => {
    return buildNativeStop({
      type: 'CameraStop',
      centerCoordinate,
      bounds,
      heading,
      pitch,
      zoomLevel,
      padding,
      animationDuration,
      animationMode
    });
  }, [centerCoordinate, bounds, heading, pitch, zoomLevel, padding, animationDuration, animationMode, buildNativeStop]);
  const nativeMaxBounds = useMemo(() => {
    if (!maxBounds?.ne || !maxBounds?.sw) {
      return null;
    }
    return JSON.stringify(makeLatLngBounds(maxBounds.ne, maxBounds.sw));
  }, [maxBounds]);
  const _setCamera = config => {
    if (!allowUpdates) {
      return;
    }
    if (!config.type)
      // @ts-expect-error The compiler doesn't understand that the `config` union type is guaranteed
      // to be an object type.
      config = {
        ...config,
        // @ts-expect-error Allows JS files to pass in an invalid config (lacking the `type` property),
        // which would raise a compilation error in TS files.
        type: config.stops ? 'CameraStops' : 'CameraStop'
      };
    if (config.type === 'CameraStops') {
      for (const _stop of config.stops) {
        let _nativeStops = [];
        const _nativeStop = buildNativeStop(_stop);
        if (_nativeStop) {
          _nativeStops = [..._nativeStops, _nativeStop];
        }
        commands.call('updateCameraStop', [{
          stops: _nativeStops
        }]);
      }
    } else if (config.type === 'CameraStop') {
      const _nativeStop = buildNativeStop(config);
      if (_nativeStop) {
        commands.call('updateCameraStop', [_nativeStop]);
      }
    }
  };
  const setCamera = useCallback(_setCamera, [allowUpdates, buildNativeStop, commands]);
  const _fitBounds = (ne, sw, paddingConfig = 0, _animationDuration = 0) => {
    let _padding = {
      paddingTop: 0,
      paddingBottom: 0,
      paddingLeft: 0,
      paddingRight: 0
    };
    if (typeof paddingConfig === 'object') {
      if (paddingConfig.length === 2) {
        _padding = {
          paddingTop: paddingConfig[0],
          paddingBottom: paddingConfig[0],
          paddingLeft: paddingConfig[1],
          paddingRight: paddingConfig[1]
        };
      } else if (paddingConfig.length === 4) {
        _padding = {
          paddingTop: paddingConfig[0],
          paddingBottom: paddingConfig[2],
          paddingLeft: paddingConfig[3],
          paddingRight: paddingConfig[1]
        };
      }
    } else if (typeof paddingConfig === 'number') {
      _padding = {
        paddingTop: paddingConfig,
        paddingBottom: paddingConfig,
        paddingLeft: paddingConfig,
        paddingRight: paddingConfig
      };
    }
    setCamera({
      type: 'CameraStop',
      bounds: {
        ne,
        sw
      },
      padding: _padding,
      animationDuration: _animationDuration,
      animationMode: 'easeTo'
    });
  };
  const fitBounds = useCallback(_fitBounds, [setCamera]);
  const _flyTo = (_centerCoordinate, _animationDuration = 2000) => {
    setCamera({
      type: 'CameraStop',
      centerCoordinate: _centerCoordinate,
      animationDuration: _animationDuration
    });
  };
  const flyTo = useCallback(_flyTo, [setCamera]);
  const _moveTo = (_centerCoordinate, _animationDuration = 0) => {
    setCamera({
      type: 'CameraStop',
      centerCoordinate: _centerCoordinate,
      animationDuration: _animationDuration,
      animationMode: 'easeTo'
    });
  };
  const moveTo = useCallback(_moveTo, [setCamera]);
  const _zoomTo = (_zoomLevel, _animationDuration = 2000) => {
    setCamera({
      type: 'CameraStop',
      zoomLevel: _zoomLevel,
      animationDuration: _animationDuration,
      animationMode: 'flyTo'
    });
  };
  const zoomTo = useCallback(_zoomTo, [setCamera]);
  useImperativeHandle(ref, () => ({
    /**
     * Sets any camera properties, with default fallbacks if unspecified.
     *
     * @example
     * camera.current?.setCamera({
     *   centerCoordinate: [lon, lat],
     * });
     *
     * @param {CameraStop | CameraStops} config
     */
    setCamera,
    /**
     * Set the camera position to enclose the provided bounds, with optional
     * padding and duration.
     *
     * @example
     * camera.fitBounds([lon, lat], [lon, lat]);
     * camera.fitBounds([lon, lat], [lon, lat], [20, 0], 1000);
     *
     * @param {Position} ne Northeast coordinate of bounding box
     * @param {Position} sw Southwest coordinate of bounding box
     * @param {number | number[]} paddingConfig The viewport padding, specified as a number (all sides equal), a 2-item array ([vertical, horizontal]), or a 4-item array ([top, right, bottom, left])
     * @param {number} animationDuration The transition duration
     */
    fitBounds,
    /**
     * Sets the camera to center around the provided coordinate using a realistic 'travel'
     * animation, with optional duration.
     *
     * @example
     * camera.flyTo([lon, lat]);
     * camera.flyTo([lon, lat], 12000);
     *
     *  @param {Position} centerCoordinate The coordinate to center in the view
     *  @param {number} animationDuration The transition duration
     */
    flyTo,
    /**
     * Sets the camera to center around the provided coordinate, with optional duration.
     *
     * @example
     * camera.moveTo([lon, lat], 200);
     * camera.moveTo([lon, lat]);
     *
     *  @param {Position} centerCoordinate The coordinate to center in the view
     *  @param {number} animationDuration The transition duration
     */
    moveTo,
    /**
     * Zooms the camera to the provided level, with optional duration.
     *
     * @example
     * camera.zoomTo(16);
     * camera.zoomTo(16, 100);
     *
     * @param {number} zoomLevel The target zoom
     * @param {number} animationDuration The transition duration
     */
    zoomTo
  }));
  return /*#__PURE__*/_jsx(RNMBXCamera, {
    testID: 'Camera'
    // @ts-expect-error just codegen stuff
    ,
    ref: nativeCamera,
    stop: nativeStop,
    animationDuration: animationDuration,
    animationMode: animationMode,
    defaultStop: nativeDefaultStop,
    followUserLocation: followUserLocation,
    followUserMode: followUserMode,
    followZoomLevel: followZoomLevel,
    followPitch: followPitch,
    followHeading: followHeading,
    followPadding: followPadding,
    minZoomLevel: minZoomLevel,
    maxZoomLevel: maxZoomLevel,
    maxBounds: nativeMaxBounds
    // @ts-expect-error just codegen stuff
    ,
    onUserTrackingModeChange: _onUserTrackingModeChange
  });
}));
const RNMBXCamera = NativeCameraView;
//# sourceMappingURL=Camera.js.map