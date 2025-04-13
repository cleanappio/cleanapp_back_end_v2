"use strict";

import React from 'react';
import { NativeModules } from 'react-native';
import RNMBXModelLayerNativeComponent from '../specs/RNMBXModelLayerNativeComponent';
import AbstractLayer from './AbstractLayer';
import { jsx as _jsx } from "react/jsx-runtime";
const Mapbox = NativeModules.RNMBXModule;

// @{codepart-replace-start(LayerPropsCommon.codepart-tsx)}

// @{codepart-replace-end}

/**
 * ModelLayer is a style layer that renders one or more stroked polylines on the map.
 */
class ModelLayer extends AbstractLayer {
  static defaultProps = {
    sourceID: Mapbox.StyleSource.DefaultSourceID
  };
  render() {
    const props = {
      ...this.baseProps,
      sourceLayerID: this.props.sourceLayerID
    };
    return /*#__PURE__*/_jsx(RNMBXModelLayerNativeComponent, {
      ref: this.setNativeLayer,
      ...props
    });
  }
}
export default ModelLayer;
//# sourceMappingURL=ModelLayer.js.map