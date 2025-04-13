"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.UserTrackingMode = exports.Camera = void 0;
var _react = _interopRequireWildcard(require("react"));
var _reactNative = require("react-native");
var _geoUtils = require("../utils/geoUtils");
var _RNMBXCameraNativeComponent = _interopRequireDefault(require("../specs/RNMBXCameraNativeComponent"));
var _NativeRNMBXCameraModule = _interopRequireDefault(require("../specs/NativeRNMBXCameraModule"));
var _NativeCommands = require("../utils/NativeCommands");
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
function _getRequireWildcardCache(e) { if ("function" != typeof WeakMap) return null; var r = new WeakMap(), t = new WeakMap(); return (_getRequireWildcardCache = function (e) { return e ? t : r; })(e); }
function _interopRequireWildcard(e, r) { if (!r && e && e.__esModule) return e; if (null === e || "object" != typeof e && "function" != typeof e) return { default: e }; var t = _getRequireWildcardCache(r); if (t && t.has(e)) return t.get(e); var n = { __proto__: null }, a = Object.defineProperty && Object.getOwnPropertyDescriptor; for (var u in e) if ("default" !== u && {}.hasOwnProperty.call(e, u)) { var i = a ? Object.getOwnPropertyDescriptor(e, u) : null; i && (i.get || i.set) ? Object.defineProperty(n, u, i) : n[u] = e[u]; } return n.default = e, t && t.set(e, n), n; }
const NativeModule = _reactNative.NativeModules.RNMBXModule;
let UserTrackingMode = exports.UserTrackingMode = /*#__PURE__*/function (UserTrackingMode) {
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
const Camera = exports.Camera = /*#__PURE__*/(0, _react.memo)(/*#__PURE__*/(0, _react.forwardRef)((props, ref) => {
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
  const nativeCamera = (0, _react.useRef)(null);
  const commands = (0, _react.useMemo)(() => new _NativeCommands.NativeCommands(_NativeRNMBXCameraModule.default), []);
  (0, _react.useEffect)(() => {
    if (nativeCamera.current) {
      commands.setNativeRef(nativeCamera.current);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [commands, nativeCamera.current]);
  const buildNativeStop = (0, _react.useCallback)((stop, ignoreFollowUserLocation = false) => {
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
      _nativeStop.centerCoordinate = JSON.stringify((0, _geoUtils.makePoint)(stop.centerCoordinate));
    }
    if (stop.bounds && stop.bounds.ne && stop.bounds.sw) {
      const {
        ne,
        sw
      } = stop.bounds;
      _nativeStop.bounds = JSON.stringify((0, _geoUtils.makeLatLngBounds)(ne, sw));
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
  const _onUserTrackingModeChange = (0, _react.useCallback)(event => {
    if (onUserTrackingModeChange) {
      if (!event.nativeEvent.payload) {
        // @ts-expect-error see the comment above
        event.nativeEvent.payload = event.nativeEvent.payloadRenamed;
      }
      onUserTrackingModeChange(event);
    }
  }, [onUserTrackingModeChange]);
  const nativeDefaultStop = (0, _react.useMemo)(() => {
    if (!defaultSettings) {
      return null;
    }
    return buildNativeStop(defaultSettings);
  }, [defaultSettings, buildNativeStop]);
  const nativeStop = (0, _react.useMemo)(() => {
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
  const nativeMaxBounds = (0, _react.useMemo)(() => {
    if (!maxBounds?.ne || !maxBounds?.sw) {
      return null;
    }
    return JSON.stringify((0, _geoUtils.makeLatLngBounds)(maxBounds.ne, maxBounds.sw));
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
  const setCamera = (0, _react.useCallback)(_setCamera, [allowUpdates, buildNativeStop, commands]);
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
  const fitBounds = (0, _react.useCallback)(_fitBounds, [setCamera]);
  const _flyTo = (_centerCoordinate, _animationDuration = 2000) => {
    setCamera({
      type: 'CameraStop',
      centerCoordinate: _centerCoordinate,
      animationDuration: _animationDuration
    });
  };
  const flyTo = (0, _react.useCallback)(_flyTo, [setCamera]);
  const _moveTo = (_centerCoordinate, _animationDuration = 0) => {
    setCamera({
      type: 'CameraStop',
      centerCoordinate: _centerCoordinate,
      animationDuration: _animationDuration,
      animationMode: 'easeTo'
    });
  };
  const moveTo = (0, _react.useCallback)(_moveTo, [setCamera]);
  const _zoomTo = (_zoomLevel, _animationDuration = 2000) => {
    setCamera({
      type: 'CameraStop',
      zoomLevel: _zoomLevel,
      animationDuration: _animationDuration,
      animationMode: 'flyTo'
    });
  };
  const zoomTo = (0, _react.useCallback)(_zoomTo, [setCamera]);
  (0, _react.useImperativeHandle)(ref, () => ({
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
  return /*#__PURE__*/(0, _jsxRuntime.jsx)(RNMBXCamera, {
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
const RNMBXCamera = _RNMBXCameraNativeComponent.default;
//# sourceMappingURL=Camera.js.map